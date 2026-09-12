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

	"github.com/spidermeaow/graft-framework/migration"
	"github.com/spidermeaow/graft-framework/migration/postgres"
	_ "github.com/lib/pq" // PostgreSQL wire protocol, kept out of the HTTP core.
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
	if command == "migrate:rollback" {
		f.IntVar(&steps, "step", 0, "number of individual migrations; omitted rolls back last batch")
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
	migrations, err := migration.Load(os.DirFS(*dir), ".")
	if err != nil {
		return err
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return errors.New("invalid PostgreSQL connection configuration")
	}
	defer db.Close()
	runner := migration.Runner{Store: postgres.New(db), Logger: slog.New(slog.NewTextHandler(errOut, nil))}
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
	}
	if err == nil && command != "migrate:status" {
		fmt.Fprintln(out, "Migrations complete")
	}
	return err
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
