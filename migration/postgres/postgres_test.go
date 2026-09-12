package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/spidermeaow/graft-framework/migration"
	_ "github.com/lib/pq"
)

// Each integration test creates and later drops only its own uniquely named database.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GRAFT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set GRAFT_TEST_DATABASE_URL to a PostgreSQL admin URL for integration tests")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" {
		t.Fatal("GRAFT_TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	name := "graft_test_" + strings.ToLower(rand.Text())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err = admin.ExecContext(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, `DROP DATABASE "`+name+`" WITH (FORCE)`); err != nil {
			t.Error(err)
		}
	})
	u.Path = "/" + name
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func migrations(t *testing.T, files map[string]string) []migration.Migration {
	t.Helper()
	fs := fstest.MapFS{}
	for name, text := range files {
		fs[name] = &fstest.MapFile{Data: []byte(text)}
	}
	m, err := migration.Load(fs, ".")
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func testRunner(db *sql.DB) migration.Runner {
	return migration.Runner{Store: New(db), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestPostgresLifecycle(t *testing.T) {
	db := testDB(t)
	r := testRunner(db)
	ctx := context.Background()
	m := migrations(t, map[string]string{"1_tables.sql": "-- +graft Up\nCREATE TABLE machines (id INT PRIMARY KEY); CREATE TABLE events (id INT);\n-- +graft Down\nDROP TABLE events; DROP TABLE machines;", "2_seed.sql": "-- +graft Up\nINSERT INTO machines VALUES (1);\n-- +graft Down\nDELETE FROM machines WHERE id=1;", "3_more.sql": "-- +graft Up\nINSERT INTO machines VALUES (2);\n-- +graft Down\nDELETE FROM machines WHERE id=2;"})
	if err := r.Up(ctx, m[:2]); err != nil {
		t.Fatal(err)
	}
	if err := r.Up(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := r.Up(ctx, m); err != nil {
		t.Fatal(err)
	}
	status, err := r.Status(ctx, m)
	if err != nil || len(status) != 3 || status[2].Applied.Batch != 2 {
		t.Fatal(status, err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM machines").Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if err := r.Rollback(ctx, m, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM machines").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	mutated := append([]migration.Migration(nil), m...)
	mutated[0].Checksum = strings.Repeat("0", 64)
	if err := r.Rollback(ctx, mutated, 1); err == nil {
		t.Fatal("mutation accepted")
	}
	if err := r.Rollback(ctx, m, 2); err != nil {
		t.Fatal(err)
	}
	status, err = r.Status(ctx, m)
	if err != nil || status[0].Applied != nil {
		t.Fatal(status, err)
	}
}

func TestPostgresTransactionFailure(t *testing.T) {
	db := testDB(t)
	r := testRunner(db)
	ctx := context.Background()
	m := migrations(t, map[string]string{"1_failure.sql": "-- +graft Up\nCREATE TABLE should_rollback(id INT); SELECT 1/0;\n-- +graft Down\nDROP TABLE should_rollback;"})
	if err := r.Up(ctx, m); err == nil {
		t.Fatal("SQL failure accepted")
	}
	var table sql.NullString
	if err := db.QueryRow("SELECT to_regclass('public.should_rollback')::text").Scan(&table); err != nil || table.Valid {
		t.Fatal(table, err)
	}
	status, err := r.Status(ctx, m)
	if err != nil || status[0].Applied != nil {
		t.Fatal(status, err)
	}
	m = migrations(t, map[string]string{"1_failure.sql": "-- +graft Up\nCREATE TABLE should_rollback(id INT);\n-- +graft Down\nDROP TABLE should_rollback; SELECT 1/0;"})
	if err := r.Up(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := r.Rollback(ctx, m, 1); err == nil {
		t.Fatal("Down failure accepted")
	}
	if err := db.QueryRow("SELECT to_regclass('public.should_rollback')::text").Scan(&table); err != nil || !table.Valid {
		t.Fatal(table, err)
	}
	status, err = r.Status(ctx, m)
	if err != nil || status[0].Applied == nil {
		t.Fatal(status, err)
	}
}

func TestPostgresLockAndCancellation(t *testing.T) {
	db := testDB(t)
	s := New(db)
	ctx := context.Background()
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.WithLock(ctx, func(migration.Session) error { close(entered); <-release; return nil })
	}()
	<-entered
	err := New(db).WithLock(ctx, func(migration.Session) error { return nil })
	close(release)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("want lock contention, got %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	m := migrations(t, map[string]string{"1_sleep.sql": "-- +graft Up\nSELECT pg_sleep(10);\n-- +graft Down\nSELECT 1;"})
	cancelled, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := testRunner(db).Up(cancelled, m); err == nil {
		t.Fatal("cancellation ignored")
	}
	if err := s.WithLock(ctx, func(migration.Session) error { return nil }); err != nil {
		t.Fatal("lock leaked:", err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected panic")
			}
		}()
		_ = s.WithLock(ctx, func(migration.Session) error { panic("test") })
	}()
	if err := s.WithLock(ctx, func(migration.Session) error { return nil }); err != nil {
		t.Fatal("panic leaked lock:", err)
	}
}
