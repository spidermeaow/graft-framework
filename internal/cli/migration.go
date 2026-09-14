package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spidermeaow/graft-framework"
	"github.com/spidermeaow/graft-framework/migration"
	mysqlstore "github.com/spidermeaow/graft-framework/migration/mysql"
)

var migrationName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func migrationCommand(ctx context.Context, command string, args []string, out, errOut io.Writer) error {
	f := flags(command, errOut)
	dir := f.String("dir", "migrations", "migration directory")
	if command == "make:migration" {
		if err := f.Parse(args); err != nil {
			return err
		}
		if f.NArg() != 1 || !migrationName.MatchString(f.Arg(0)) {
			return errors.New("usage: graft make:migration [--dir migrations] snake_case_name")
		}
		path, err := makeMigration(*dir, f.Arg(0), time.Now())
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "Created", path)
		return nil
	}
	timeout := f.Duration("timeout", 2*time.Minute, "database operation timeout")
	steps := 0
	var pending, applied int64
	var plan, confirm bool
	var note string
	if command == "migrate:rollback" {
		f.IntVar(&steps, "step", 0, "number of individual migrations; omitted rolls back last batch")
	}
	if command == "migrate:repair" {
		f.Int64Var(&pending, "mark-pending", 0, "dirty version verified to be fully pending")
		f.Int64Var(&applied, "mark-applied", 0, "dirty version verified to be fully applied")
		f.BoolVar(&plan, "plan", false, "print a read-only repair plan")
		f.BoolVar(&confirm, "confirm", false, "apply a reviewed repair plan")
		f.StringVar(&note, "note", "", "operator reason recorded in audit history")
	}
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *timeout <= 0 || steps < 0 {
		return errors.New("timeout must be positive and step nonnegative")
	}
	if command == "migrate:repair" && ((pending <= 0 && applied <= 0) || (pending > 0 && applied > 0) || (pending < 0 || applied < 0) || plan == confirm) {
		return errors.New("usage: graft migrate:repair (--mark-pending VERSION | --mark-applied VERSION) (--plan | --confirm --note REASON)")
	}
	if command == "migrate:repair" && confirm && strings.TrimSpace(note) == "" {
		return errors.New("repair requires --note REASON with --confirm")
	}
	if err := graft.LoadEnv(); err != nil {
		return err
	}
	dialect, err := migrationDriver(os.Getenv("DATABASE_DRIVER"))
	if err != nil {
		return err
	}
	if command == "migrate:repair" && dialect != "mysql" {
		return errors.New("migrate:repair is for MySQL dirty migrations; PostgreSQL rolls back failed migrations transactionally")
	}
	migrations, loadErr := migration.LoadDialect(os.DirFS(*dir), ".", dialect)
	if loadErr != nil && command != "migrate:doctor" {
		return loadErr
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	db, store, err := openMigrationStore(dialect, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	runner := migration.Runner{Store: store, Logger: slog.New(slog.NewTextHandler(errOut, nil))}
	switch command {
	case "migrate":
		err = runner.Up(ctx, migrations)
	case "migrate:rollback":
		err = runner.Rollback(ctx, migrations, steps)
	case "migrate:status":
		var statuses []migration.Status
		statuses, err = runner.Status(ctx, migrations)
		if err != nil {
			break
		}
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "VERSION\tNAME\tSTATE\tBATCH\tDURATION")
		for _, status := range statuses {
			state, batch, duration := "pending", 0, time.Duration(0)
			if status.Applied != nil {
				state, batch, duration = "applied", status.Applied.Batch, status.Applied.ExecutionTime
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%s\n", status.Migration.Version, status.Migration.Name, state, batch, duration)
		}
		err = w.Flush()
	case "migrate:doctor":
		if loadErr != nil {
			fmt.Fprintf(out, "Local migration files could not be read: %v\n", loadErr)
		}
		if dialect == "mysql" {
			err = mysqlDoctor(ctx, store.(*mysqlstore.Store), migrations, out)
			if err == nil && loadErr != nil {
				err = loadErr
			}
		} else {
			if loadErr != nil {
				err = loadErr
			} else {
				_, err = runner.Status(ctx, migrations)
			}
			if err == nil {
				fmt.Fprintln(out, "PostgreSQL migration history is healthy; failed SQL rolls back transactionally.")
			}
		}
	case "migrate:repair":
		target, version := "pending", pending
		if applied > 0 {
			target, version = "applied", applied
		}
		var local *migration.Migration
		for i := range migrations {
			if migrations[i].Version == version {
				local = &migrations[i]
				break
			}
		}
		if local == nil {
			return fmt.Errorf("local migration %d is missing; repair refused", version)
		}
		mysqlStore := store.(*mysqlstore.Store)
		var dirty mysqlstore.DirtyMigration
		dirty, err = mysqlStore.RepairPlan(ctx, *local, target)
		if err != nil {
			break
		}
		fmt.Fprintf(out, "Dirty migration: %d_%s (%s)\n", dirty.Version, dirty.Name, dirty.Direction)
		fmt.Fprintf(out, "Plan: mark %s after verifying the database fully matches that state.\n", target)
		if plan {
			break
		}
		err = mysqlStore.Repair(ctx, *local, target, note)
		if err == nil {
			fmt.Fprintf(out, "Repaired migration %d as %s. Audit note recorded.\n", version, target)
		}
	}
	if err == nil && (command == "migrate" || command == "migrate:rollback") {
		fmt.Fprintln(out, "Migrations complete")
	}
	return err
}
