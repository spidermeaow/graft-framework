// Package postgres implements migration.Store using database/sql and PostgreSQL.
// The application owns its *sql.DB and registers its preferred PostgreSQL driver.
package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/spidermeaow/graft-framework/migration"
)

// ErrLocked indicates another Graft migration command owns this database's lock.
var ErrLocked = errors.New("another Graft migration operation is running")

// Store persists history in public.graft_migrations, using a dedicated connection
// and session advisory lock. Use a direct connection or session-mode pooler.
type Store struct{ db *sql.DB }

// New uses an existing connection pool without taking ownership of it.
func New(db *sql.DB) *Store { return &Store{db: db} }

const lockID int64 = 0x4772616674
const createHistory = `CREATE TABLE IF NOT EXISTS public.graft_migrations (
version BIGINT PRIMARY KEY,
name TEXT NOT NULL,
batch INTEGER NOT NULL CHECK (batch > 0),
executed_at TIMESTAMPTZ NOT NULL,
execution_time BIGINT NOT NULL,
checksum TEXT NOT NULL
)`

// WithLock serializes history reads and changes on a single database connection.
func (s *Store) WithLock(ctx context.Context, fn func(migration.Session) error) (err error) {
	if s.db == nil {
		return errors.New("postgres: nil database")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked bool
	if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&locked); err != nil {
		// The server may have acquired the lock before cancellation reached the client.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return err
	}
	if !locked {
		return ErrLocked
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		unlockErr := conn.QueryRowContext(cleanup, "SELECT pg_advisory_unlock($1)", lockID).Scan(&unlocked)
		if unlockErr != nil || !unlocked {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			if unlockErr == nil {
				unlockErr = errors.New("migration advisory lock was lost")
			}
			err = errors.Join(err, unlockErr)
		}
	}()
	if _, err = conn.ExecContext(ctx, createHistory); err != nil {
		return fmt.Errorf("create migration history: %w", err)
	}
	return fn(&session{conn: conn})
}

type session struct{ conn *sql.Conn }

func (s *session) History(ctx context.Context) ([]migration.Record, error) {
	rows, err := s.conn.QueryContext(ctx, "SELECT version,name,batch,executed_at,execution_time,checksum FROM public.graft_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var history []migration.Record
	for rows.Next() {
		var r migration.Record
		if err := rows.Scan(&r.Version, &r.Name, &r.Batch, &r.ExecutedAt, &r.ExecutionTime, &r.Checksum); err != nil {
			return nil, err
		}
		history = append(history, r)
	}
	return history, rows.Err()
}
func (s *session) Apply(ctx context.Context, m migration.Migration, batch int) (migration.Record, error) {
	r := migration.Record{Version: m.Version, Name: m.Name, Batch: batch, Checksum: m.Checksum}
	start := time.Now()
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, m.Up); err != nil {
		return r, err
	}
	r.ExecutedAt = time.Now().UTC()
	r.ExecutionTime = time.Since(start)
	_, err = tx.ExecContext(ctx, "INSERT INTO public.graft_migrations (version,name,batch,executed_at,execution_time,checksum) VALUES ($1,$2,$3,$4,$5,$6)", r.Version, r.Name, r.Batch, r.ExecutedAt, int64(r.ExecutionTime), r.Checksum)
	if err != nil {
		return r, err
	}
	return r, tx.Commit()
}
func (s *session) Revert(ctx context.Context, m migration.Migration) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, m.Down); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM public.graft_migrations WHERE version=$1 AND checksum=$2", m.Version, m.Checksum)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("migration history changed during rollback")
	}
	return tx.Commit()
}
