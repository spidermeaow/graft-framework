package mysql

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/spidermeaow/graft-framework/migration"
)

// The configured account must create/drop databases. Only a unique test database
// is migrated and dropped; the database named in the DSN is never modified.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GRAFT_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set GRAFT_TEST_MYSQL_DSN to a MySQL admin DSN")
	}
	cfg, err := driver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test MySQL DSN")
	}
	cfg.ParseTime, cfg.MultiStatements, cfg.Loc = true, true, time.UTC
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	name := "graft_test_" + strings.ToLower(rand.Text())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+name+"`"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP DATABASE `"+name+"`"); err != nil {
			t.Error(err)
		}
	})
	cfg.DBName = name
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func load(t *testing.T, files map[string]string) []migration.Migration {
	t.Helper()
	fs := fstest.MapFS{}
	for name, sql := range files {
		fs[name] = &fstest.MapFile{Data: []byte(sql)}
	}
	m, err := migration.LoadDialect(fs, ".", "mysql")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLifecycle(t *testing.T) {
	db := testDB(t)
	r := migration.Runner{Store: New(db)}
	ctx := context.Background()
	m := load(t, map[string]string{
		"1_tables.sql": "-- +graft Up\nCREATE TABLE `items` (id BIGINT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(255)); CREATE TABLE events (id INT);\n-- +graft Down\nDROP TABLE events; DROP TABLE items;",
		"2_seed.sql":   "-- +graft Up\nINSERT INTO items(name) VALUES ('hello;world');\n-- +graft Down\nDELETE FROM items;",
		"3_more.sql":   "-- +graft Up\nINSERT INTO events VALUES (1);\n-- +graft Down\nDELETE FROM events;",
	})
	for _, set := range [][]migration.Migration{m[:2], m, m} {
		if err := r.Up(ctx, set); err != nil {
			t.Fatal(err)
		}
	}
	status, err := r.Status(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 3 || status[2].Applied == nil || status[2].Applied.Batch != 2 {
		t.Fatal(status)
	}
	mutated := append([]migration.Migration(nil), m...)
	mutated[0].Checksum = strings.Repeat("0", 64)
	if err := r.Up(ctx, mutated); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	if err := r.Rollback(ctx, m, 0); err != nil {
		t.Fatal(err)
	}
	status, err = r.Status(ctx, m)
	if err != nil || status[2].Applied != nil || status[1].Applied == nil {
		t.Fatal(status, err)
	}
	if err := r.Rollback(ctx, m, 2); err != nil {
		t.Fatal(err)
	}
	status, err = r.Status(ctx, m)
	if err != nil || status[0].Applied != nil {
		t.Fatal(status, err)
	}
}

func TestDirtyFailureAndRepair(t *testing.T) {
	for _, direction := range []string{"up", "down"} {
		t.Run(direction, func(t *testing.T) {
			db := testDB(t)
			r := migration.Runner{Store: New(db)}
			ctx := context.Background()
			up, down := "CREATE TABLE partial (id INT);", "DROP TABLE partial;"
			if direction == "up" {
				up += " INSERT INTO nonexistent_graft_table VALUES (1);"
			} else {
				down += " INSERT INTO nonexistent_graft_table VALUES (1);"
			}
			m := load(t, map[string]string{"1_partial.sql": "-- +graft Up\n" + up + "\n-- +graft Down\n" + down})
			err := r.Up(ctx, m)
			if direction == "down" {
				if err != nil {
					t.Fatal(err)
				}
				err = r.Rollback(ctx, m, 1)
			}
			var dirty *DirtyError
			if !errors.As(err, &dirty) || dirty.Direction != direction {
				t.Fatalf("expected dirty %s, got %v", direction, err)
			}
			for _, op := range []func() error{
				func() error { return r.Up(ctx, m) },
				func() error { return r.Rollback(ctx, m, 1) },
				func() error { _, err := r.Status(ctx, m); return err },
			} {
				if err := op(); !errors.As(err, &dirty) {
					t.Fatalf("dirty operation accepted: %v", err)
				}
			}
			var tables int
			if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='partial'").Scan(&tables); err != nil {
				t.Fatal(err)
			}
			if (direction == "up" && tables != 1) || (direction == "down" && tables != 0) {
				t.Fatal("DDL did not partially commit", tables)
			}
			// Exercise the documented restore-to-pre-operation recovery procedure.
			if direction == "up" {
				if _, err := db.Exec("DROP TABLE partial; DELETE FROM graft_migrations WHERE version=1 AND dirty='up'"); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := db.Exec("CREATE TABLE partial (id INT); UPDATE graft_migrations SET dirty='' WHERE version=1 AND dirty='down'"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.Status(ctx, m); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLockAndCancellation(t *testing.T) {
	db := testDB(t)
	s := New(db)
	ctx := context.Background()
	if err := s.WithLock(ctx, func(migration.Session) error {
		err := New(db).WithLock(ctx, func(migration.Session) error { t.Error("contender acquired lock"); return nil })
		if !errors.Is(err, ErrLocked) {
			t.Errorf("expected contention, got %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}
	m := load(t, map[string]string{"1_sleep.sql": "-- +graft Up\nSELECT SLEEP(2);\n-- +graft Down\nSELECT 1;"})
	cancelled, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if err := (migration.Runner{Store: s}).Up(cancelled, m); err == nil {
		t.Fatal("cancellation ignored")
	}
	// A disconnected MySQL session can finish its running statement before the
	// server observes the disconnect and releases the named lock.
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := s.WithLock(ctx, func(session migration.Session) error {
			_, err := session.History(ctx)
			var dirty *DirtyError
			if !errors.As(err, &dirty) {
				t.Errorf("expected persistent dirty marker, got %v", err)
			}
			return nil
		})
		if err == nil {
			break
		}
		if !errors.Is(err, ErrLocked) || time.Now().After(deadline) {
			t.Fatal("lock leaked:", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestNilStore(t *testing.T) {
	if err := New(nil).WithLock(context.Background(), func(migration.Session) error { return nil }); err == nil {
		t.Fatal("nil accepted")
	}
}

func TestSessionConfiguration(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, setting := range []string{"SET SESSION autocommit=0", "SET SESSION sql_mode='NO_BACKSLASH_ESCAPES'", "SET SESSION sql_mode='ANSI_QUOTES'"} {
		if _, err := db.Exec("SET SESSION autocommit=1; SET SESSION sql_mode=''"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(setting); err != nil {
			t.Fatal(err)
		}
		if err := New(db).WithLock(context.Background(), func(migration.Session) error { t.Error("unsupported settings accepted"); return nil }); err == nil {
			t.Fatal(setting)
		}
	}
}
