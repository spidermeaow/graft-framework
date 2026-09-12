package migration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

func fixture(t *testing.T) []Migration {
	t.Helper()
	m, err := Load(fstest.MapFS{"10_third.sql": {Data: []byte("-- +graft Up\nSELECT 10;\n-- +graft Down\nSELECT 0;")}, "2_second.sql": {Data: []byte("-- +graft Up\nSELECT 2;\n-- +graft Down\nSELECT 0;")}, "1_first.sql": {Data: []byte("-- +graft Up\nSELECT 1;\n-- +graft Down\nSELECT 0;")}}, ".")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

type memoryStore struct {
	mu                sync.Mutex
	records           []Record
	applied, reverted []int64
	fail              int64
}

func (s *memoryStore) WithLock(ctx context.Context, fn func(Session) error) error {
	if !s.mu.TryLock() {
		return errors.New("locked")
	}
	defer s.mu.Unlock()
	return fn(s)
}
func (s *memoryStore) History(context.Context) ([]Record, error) {
	return append([]Record(nil), s.records...), nil
}
func (s *memoryStore) Apply(_ context.Context, m Migration, batch int) (Record, error) {
	if s.fail == m.Version {
		return Record{}, errors.New("SQL failed")
	}
	r := Record{Version: m.Version, Name: m.Name, Batch: batch, Checksum: m.Checksum, ExecutedAt: time.Now(), ExecutionTime: time.Millisecond}
	s.records = append(s.records, r)
	s.applied = append(s.applied, m.Version)
	return r, nil
}
func (s *memoryStore) Revert(_ context.Context, m Migration) error {
	if s.fail == m.Version {
		return errors.New("SQL failed")
	}
	for i, r := range s.records {
		if r.Version == m.Version {
			s.records = append(s.records[:i], s.records[i+1:]...)
			break
		}
	}
	s.reverted = append(s.reverted, m.Version)
	return nil
}
func runner(s Store) Runner {
	return Runner{Store: s, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestOrderingHistoryAndRollback(t *testing.T) {
	m := fixture(t)
	if m[0].Version != 1 || m[1].Version != 2 || m[2].Version != 10 {
		t.Fatal(m)
	}
	s := &memoryStore{}
	r := runner(s)
	ctx := context.Background()
	if err := r.Up(ctx, m[:2]); err != nil {
		t.Fatal(err)
	}
	if err := r.Up(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := r.Up(ctx, m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.applied, []int64{1, 2, 10}) {
		t.Fatal(s.applied)
	}
	status, err := r.Status(ctx, m)
	if err != nil || len(status) != 3 || status[2].Applied.Batch != 2 {
		t.Fatalf("%+v %v", status, err)
	}
	if err := r.Rollback(ctx, m, 0); err != nil {
		t.Fatal(err)
	}
	if len(s.records) != 2 || s.reverted[0] != 10 {
		t.Fatal(s)
	}
	if err := r.Rollback(ctx, m, 2); err != nil {
		t.Fatal(err)
	}
	if len(s.records) != 0 || !reflect.DeepEqual(s.reverted, []int64{10, 2, 1}) {
		t.Fatal(s)
	}
	status, err = r.Status(ctx, m)
	if err != nil || status[0].Applied != nil {
		t.Fatal(status, err)
	}
}

func TestBatchFailureAndRetry(t *testing.T) {
	m := fixture(t)
	s := &memoryStore{fail: 2}
	r := runner(s)
	ctx := context.Background()
	if err := r.Up(ctx, m); err == nil {
		t.Fatal("expected failure")
	}
	if len(s.records) != 1 {
		t.Fatal(s.records)
	}
	s.fail = 0
	if err := r.Up(ctx, m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.applied, []int64{1, 2, 10}) {
		t.Fatal(s.applied)
	}
	s.fail = 10
	if err := r.Rollback(ctx, m, 1); err == nil || len(s.records) != 3 {
		t.Fatal("failed Down changed history")
	}
}

func TestMutationMissingAndOutOfOrder(t *testing.T) {
	for _, mode := range []string{"checksum", "name", "missing", "retroactive"} {
		t.Run(mode, func(t *testing.T) {
			m := fixture(t)
			s := &memoryStore{}
			r := runner(s)
			ctx := context.Background()
			initial := m
			if mode == "retroactive" {
				initial = m[1:]
			}
			if err := r.Up(ctx, initial); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "checksum":
				m[0].Checksum = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			case "name":
				m[0].Name = "changed"
			case "missing":
				m = m[1:]
			}
			if err := r.Up(ctx, m); err == nil {
				t.Fatal("drift accepted")
			}
			if _, err := r.Status(ctx, m); err == nil {
				t.Fatal("status ignored drift")
			}
			if err := r.Rollback(ctx, m, 1); err == nil {
				t.Fatal("rollback ignored drift")
			}
		})
	}
}

func TestConcurrentProtection(t *testing.T) {
	s := &memoryStore{}
	r := runner(s)
	m := fixture(t)
	s.mu.Lock()
	if err := r.Up(context.Background(), m); err == nil {
		t.Fatal("concurrent operation accepted")
	}
	s.mu.Unlock()
	if err := r.Up(context.Background(), m); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidMigration(t *testing.T) {
	valid := "-- +graft Up\nSELECT 1;\n-- +graft Down\nSELECT 2;"
	for _, tt := range []struct{ name, sql string }{{"bad.sql", valid}, {"0_zero.sql", valid}, {"999999999999999999999_large.sql", valid}, {"1_missing.sql", "-- +graft Up\nSELECT 1;"}, {"1_empty.sql", "-- +graft Up\n-- no SQL\n-- +graft Down\nSELECT 1;"}, {"1_order.sql", "-- +graft Down\nSELECT 1;\n-- +graft Up\nSELECT 1;"}, {"1_transaction.sql", "-- +graft Up\nCOMMIT;\n-- +graft Down\nSELECT 1;"}} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(fstest.MapFS{tt.name: {Data: []byte(tt.sql)}}, "."); err == nil {
				t.Fatal("invalid migration accepted")
			}
		})
	}
	if _, err := Load(fstest.MapFS{"01_a.sql": {Data: []byte(valid)}, "1_b.sql": {Data: []byte(valid)}}, "."); err == nil {
		t.Fatal("duplicate version accepted")
	}
}

func TestSQLTransactionGuard(t *testing.T) {
	for _, sql := range []string{"BEGIN;SELECT 1;", "SELECT 1; /* hi */ COMMIT;", "ROLLBACK;", "START TRANSACTION;", "-- comment\nEND;", "/* nested /* inner */ comment */ ABORT;", "PREPARE TRANSACTION 'x';"} {
		if err := validateSQL(sql); err == nil {
			t.Errorf("accepted %q", sql)
		}
	}
	for _, sql := range []string{"SELECT 'COMMIT;';", "DO $$ BEGIN RAISE NOTICE 'ok'; END $$;", "CREATE FUNCTION x() RETURNS void AS $body$ BEGIN RETURN; END $body$ LANGUAGE plpgsql;", `SELECT E'escaped\'string;';`, "SELECT 1; -- COMMIT;"} {
		if err := validateSQL(sql); err != nil {
			t.Errorf("%s: %v", sql, err)
		}
	}
}
