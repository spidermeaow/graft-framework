package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	mysqlstore "github.com/spidermeaow/graft-framework/migration/mysql"
	"github.com/spidermeaow/graft-framework/migration/postgres"
)

func TestMySQLCommandsFromDotEnv(t *testing.T) {
	dsn := os.Getenv("GRAFT_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set GRAFT_TEST_MYSQL_DSN for MySQL CLI integration")
	}
	cfg, err := driver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	name := "graft_test_cli_" + strings.ToLower(rand.Text())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+name+"`"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanup, "DROP DATABASE `"+name+"`"); err != nil {
			t.Error(err)
		}
	})
	cfg.DBName = name
	// CLI must enable parseTime and multiStatements itself.
	cfg.ParseTime, cfg.MultiStatements = false, false
	t.Chdir(t.TempDir())
	for _, key := range []string{"DATABASE_DRIVER", "DATABASE_URL"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(".env", []byte("DATABASE_DRIVER=mysql\nDATABASE_URL='"+cfg.FormatDSN()+"'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(ctx, []string{"make:migration", "create_items"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("migrations")
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	if err := os.WriteFile("migrations/"+entries[0].Name(), []byte("-- +graft Up\nCREATE TABLE items (id INT); INSERT INTO items VALUES (1);\n-- +graft Down\nDELETE FROM items; DROP TABLE items;"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"migrate:status"}, {"migrate"}, {"migrate"}, {"migrate:status"}, {"migrate:rollback", "--step", "1"}, {"migrate:status"}} {
		out.Reset()
		if err := Run(ctx, command, &out, io.Discard); err != nil {
			t.Fatalf("%v: %v", command, err)
		}
		if command[0] == "migrate:status" && !strings.Contains(out.String(), "create_items") {
			t.Fatal(out.String())
		}
	}
	if !strings.Contains(out.String(), "pending") {
		t.Fatal(out.String())
	}
}

func TestMigrationDriver(t *testing.T) {
	for input, want := range map[string]string{"": "postgres", "postgresql": "postgres", "postgres": "postgres", " MySQL ": "mysql"} {
		got, err := migrationDriver(input)
		if err != nil || got != want {
			t.Fatal(input, got, err)
		}
	}
	if _, err := migrationDriver("sqlite"); err == nil {
		t.Fatal("unsupported driver accepted")
	}
}

func TestOpenMigrationStore(t *testing.T) {
	db, store, err := openMigrationStore("mysql", "user:secret@tcp(localhost:3306)/app")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, ok := store.(*mysqlstore.Store); !ok {
		t.Fatalf("wrong store %T", store)
	}
	db2, store, err := openMigrationStore("postgres", "postgres://localhost/app")
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if _, ok := store.(*postgres.Store); !ok {
		t.Fatalf("wrong store %T", store)
	}
	for _, dsn := range []string{"user:secret@tcp(localhost:3306)/", "user:secret@tcp(broken"} {
		_, _, err := openMigrationStore("mysql", dsn)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid/sensitive diagnostic: %v", err)
		}
	}
}
