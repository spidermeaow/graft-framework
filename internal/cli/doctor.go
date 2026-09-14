package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spidermeaow/graft-framework"
	"github.com/spidermeaow/graft-framework/migration"
)

// doctorCommand validates a project without changing its files or migration
// history. A database ping is performed only when DATABASE_URL is configured.
func doctorCommand(ctx context.Context, args []string, out, errOut io.Writer) error {
	f := flags("doctor", errOut)
	timeout := f.Duration("timeout", 5*time.Second, "database connection timeout")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *timeout <= 0 {
		return errors.New("usage: graft doctor [--timeout 5s]")
	}

	result := doctorResult{out: out}
	fmt.Fprintln(out, "Graft doctor")
	doctorGo(ctx, &result)
	doctorProject(&result)
	if err := graft.LoadEnv(); err != nil {
		result.fail("Environment", err)
	} else if doctorEnvFileExists() {
		result.ok("Environment", ".env loaded")
	} else {
		result.warn("Environment", "no .env file found; shell variables are still supported")
	}

	cfg, configErr := graft.ConfigFromEnv()
	if configErr != nil {
		result.fail("App configuration", configErr)
	} else {
		result.ok("App configuration", "listening on "+cfg.Address())
	}
	doctorMigrations(&result)
	doctorDatabase(ctx, *timeout, &result)

	if len(result.problems) == 0 {
		fmt.Fprintln(out, "\nDoctor found no blocking issues.")
		return nil
	}
	fmt.Fprintf(out, "\nDoctor found %d issue(s). Fix the failed checks and run it again.\n", len(result.problems))
	return errors.Join(result.problems...)
}

type doctorResult struct {
	out      io.Writer
	problems []error
}

func (r *doctorResult) ok(name, detail string)   { fmt.Fprintf(r.out, "[ok] %s: %s\n", name, detail) }
func (r *doctorResult) warn(name, detail string) { fmt.Fprintf(r.out, "[warn] %s: %s\n", name, detail) }
func (r *doctorResult) fail(name string, err error) {
	fmt.Fprintf(r.out, "[fail] %s: %v\n", name, err)
	r.problems = append(r.problems, fmt.Errorf("%s: %w", strings.ToLower(name), err))
}

func doctorGo(ctx context.Context, result *doctorResult) {
	output, err := exec.CommandContext(ctx, "go", "version").Output()
	if err != nil {
		result.fail("Go toolchain", errors.New("Go was not found on PATH; install Go to use graft dev, build, or publish"))
		return
	}
	result.ok("Go toolchain", strings.TrimSpace(string(output)))
}

func doctorProject(result *doctorResult) {
	data, err := os.ReadFile("go.mod")
	if os.IsNotExist(err) {
		result.warn("Project", "no go.mod found; run this inside a Graft project for full checks")
		return
	}
	if err != nil {
		result.fail("Project", err)
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "module ") {
			result.ok("Project", strings.TrimSpace(line))
			return
		}
	}
	result.fail("Project", errors.New("go.mod has no module declaration"))
}

func doctorEnvFileExists() bool {
	paths := []string{".env"}
	if executable, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(executable), ".env"))
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func doctorMigrations(result *doctorResult) {
	info, err := os.Stat("migrations")
	if os.IsNotExist(err) {
		result.warn("Migrations", "no migrations directory found")
		return
	}
	if err != nil || !info.IsDir() {
		result.fail("Migrations", errors.New("migrations must be a directory"))
		return
	}
	dialect, err := migrationDriver(os.Getenv("DATABASE_DRIVER"))
	if err != nil {
		result.fail("Migrations", err)
		return
	}
	migrations, err := migration.LoadDialect(os.DirFS("migrations"), ".", dialect)
	if err != nil {
		result.fail("Migrations", err)
		return
	}
	result.ok("Migrations", fmt.Sprintf("%d local %s migration(s)", len(migrations), dialect))
}

func doctorDatabase(ctx context.Context, timeout time.Duration, result *doctorResult) {
	dsn, driver := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_DRIVER")
	if dsn == "" && driver == "" {
		result.warn("Database", "DATABASE_URL is not set; connection check skipped")
		return
	}
	if dsn == "" {
		result.fail("Database", errors.New("DATABASE_URL is required when DATABASE_DRIVER is set"))
		return
	}
	dialect, err := migrationDriver(driver)
	if err != nil {
		result.fail("Database", err)
		return
	}
	db, _, err := openMigrationStore(dialect, dsn)
	if err != nil {
		result.fail("Database", err)
		return
	}
	defer db.Close()
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		result.fail("Database", fmt.Errorf("cannot connect to %s: %w", dialect, err))
		return
	}
	result.ok("Database", dialect+" connection succeeded")
}
