package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq" // PostgreSQL wire protocol, kept out of the HTTP core.
	"github.com/spidermeaow/graft-framework"
	"github.com/spidermeaow/graft-framework/migration"
	mysqlstore "github.com/spidermeaow/graft-framework/migration/mysql"
	"github.com/spidermeaow/graft-framework/migration/postgres"
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

func mysqlDoctor(ctx context.Context, store *mysqlstore.Store, migrations []migration.Migration, out io.Writer) error {
	dirty, err := store.Inspect(ctx)
	if err != nil {
		return err
	}
	if len(dirty) == 0 {
		fmt.Fprintln(out, "No dirty MySQL migrations. Run graft migrate:status for full history.")
		return nil
	}
	local := make(map[int64]migration.Migration, len(migrations))
	for _, m := range migrations {
		local[m.Version] = m
	}
	for _, d := range dirty {
		fmt.Fprintf(out, "DIRTY %d_%s (%s), last event: %s\n", d.Version, d.Name, d.Direction, d.LastEvent)
		if m, ok := local[d.Version]; !ok || m.Name != d.Name || m.Checksum != d.Checksum {
			fmt.Fprintln(out, "  Local SQL is missing or differs from recorded name/checksum; restore the exact file before repair.")
		}
		fmt.Fprintln(out, "  Back up and inspect actual schema/data. Restore fully to pending or applied state before changing history.")
		fmt.Fprintf(out, "  Preview: graft migrate:repair --mark-pending %d --plan (or --mark-applied %d --plan)\n", d.Version, d.Version)
	}
	return &mysqlstore.DirtyError{Version: dirty[0].Version, Direction: dirty[0].Direction}
}

func migrationDriver(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "postgres", "postgresql":
		return "postgres", nil
	case "mysql":
		return "mysql", nil
	default:
		return "", errors.New("unsupported DATABASE_DRIVER; use postgres or mysql")
	}
}

func openMigrationStore(dialect, dsn string) (*sql.DB, migration.Store, error) {
	if dialect == "mysql" {
		cfg, err := mysqldriver.ParseDSN(dsn)
		if err != nil || cfg.DBName == "" {
			return nil, nil, errors.New("invalid MySQL DATABASE_URL; expected user:password@tcp(host:3306)/database")
		}
		cfg.ParseTime, cfg.MultiStatements, cfg.Loc = true, true, time.UTC
		connector, err := mysqldriver.NewConnector(cfg)
		if err != nil {
			return nil, nil, errors.New("invalid MySQL connection configuration")
		}
		db := sql.OpenDB(connector)
		return db, mysqlstore.New(db), nil
	}
	if dialect != "postgres" {
		return nil, nil, errors.New("unsupported DATABASE_DRIVER; use postgres or mysql")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, nil, errors.New("invalid PostgreSQL connection configuration")
	}
	return db, postgres.New(db), nil
}

func makeMigration(dir, name string, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	lockPath := filepath.Join(dir, ".graft-create.lock")
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("lock migration directory (another generator may be running): %w", err)
	}
	defer os.Remove(lockPath)
	if err := lock.Close(); err != nil {
		return "", err
	}
	version, _ := strconv.ParseInt(now.UTC().Format("20060102150405"), 10, 64)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		v, e := strconv.ParseInt(prefix, 10, 64)
		if e == nil && v >= version {
			if v == 1<<63-1 {
				return "", errors.New("migration version overflow")
			}
			version = v + 1
		}
	}
	// O_EXCL also prevents overwrites if two generators choose the same name/version.
	path := filepath.Join(dir, fmt.Sprintf("%d_%s.sql", version, name))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", err
	}
	_, writeErr := file.WriteString("-- +graft Up\n\n\n-- +graft Down\n\n")
	closeErr := file.Close()
	return path, errors.Join(writeErr, closeErr)
}
