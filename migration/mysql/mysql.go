// Package mysql implements SQL-first migrations for MySQL 8.0+ using database/sql.
// DDL is not transactional: failures leave a durable dirty marker requiring repair.
package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spidermeaow/graft-framework/migration"
)

// ErrLocked means another session owns this database's migration lock.
var ErrLocked = errors.New("another Graft migration operation is running")

// DirtyError identifies an interrupted operation. SQL may already be committed.
type DirtyError struct {
	Version   int64
	Direction string
}

func (e *DirtyError) Error() string {
	return fmt.Sprintf("MySQL migration %d is dirty (%s); SQL may be partially committed: restore the pre-operation schema/data and repair graft_migrations before retrying (see docs/MYSQL.md)", e.Version, e.Direction)
}

// Store uses a database-scoped named lock on a dedicated session.
// The supplied pool must use parseTime=true, loc=UTC, multiStatements=true and
// autocommit=1. Connect directly to the writable server, not a transaction pooler.
type Store struct{ db *sql.DB }

// New does not take ownership of db. The caller registers its database/sql driver.
func New(db *sql.DB) *Store { return &Store{db: db} }

const createHistory = `CREATE TABLE IF NOT EXISTS graft_migrations (
version BIGINT PRIMARY KEY,
name VARCHAR(255) NOT NULL,
batch INT NOT NULL,
executed_at DATETIME(6) NOT NULL,
execution_time BIGINT NOT NULL,
checksum CHAR(64) NOT NULL,
dirty VARCHAR(4) NOT NULL DEFAULT ''
) ENGINE=InnoDB`

const createEvents = `CREATE TABLE IF NOT EXISTS graft_migration_events (
id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
version BIGINT NOT NULL,
direction VARCHAR(4) NOT NULL,
event VARCHAR(16) NOT NULL,
created_at DATETIME(6) NOT NULL,
note VARCHAR(1000) NOT NULL DEFAULT ''
) ENGINE=InnoDB`

func (s *Store) WithLock(ctx context.Context, fn func(migration.Session) error) (err error) {
	return s.withLock(ctx, true, fn)
}

func (s *Store) withLock(ctx context.Context, initialize bool, fn func(migration.Session) error) (err error) {
	if s.db == nil {
		return errors.New("mysql: nil database")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var database sql.NullString
	var autocommit bool
	var mode string
	if err = conn.QueryRowContext(ctx, "SELECT DATABASE(), @@SESSION.autocommit, @@SESSION.sql_mode").Scan(&database, &autocommit, &mode); err != nil {
		return err
	}
	if !database.Valid || database.String == "" {
		return errors.New("mysql: DATABASE_URL must select a database")
	}
	if !autocommit {
		return errors.New("mysql migrations require autocommit=1")
	}
	if strings.Contains(mode, "NO_BACKSLASH_ESCAPES") || strings.Contains(mode, "ANSI_QUOTES") {
		return errors.New("mysql migrations do not support NO_BACKSLASH_ESCAPES or ANSI_QUOTES sql_mode")
	}
	// Case folding intentionally also serializes case-distinct database names.
	hash := sha256.Sum256([]byte(strings.ToLower(database.String)))
	lock := fmt.Sprintf("graft:%x", hash[:28])
	var locked sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", lock).Scan(&locked); err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return err
	}
	if !locked.Valid {
		return errors.New("mysql: could not acquire migration lock")
	}
	if locked.Int64 != 1 {
		return ErrLocked
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked sql.NullInt64
		unlockErr := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(?)", lock).Scan(&unlocked)
		if unlockErr != nil || !unlocked.Valid || unlocked.Int64 != 1 {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			if unlockErr == nil {
				unlockErr = errors.New("mysql migration lock was lost")
			}
			err = errors.Join(err, unlockErr)
		}
	}()
	if initialize {
		if _, err = conn.ExecContext(ctx, createHistory); err != nil {
			return fmt.Errorf("create migration history: %w", err)
		}
		if _, err = conn.ExecContext(ctx, createEvents); err != nil {
			return fmt.Errorf("create migration event log: %w", err)
		}
	}
	return fn(&session{conn: conn})
}

type session struct{ conn *sql.Conn }

func (s *session) event(ctx context.Context, version int64, direction, event, note string) error {
	_, err := s.conn.ExecContext(ctx, "INSERT INTO graft_migration_events (version,direction,event,created_at,note) VALUES (?,?,?,?,?)", version, direction, event, time.Now().UTC(), note)
	return err
}

func failureNote(err error) string {
	// SQL driver errors can include SQL fragments, which may contain secrets.
	// The original error still reaches the caller; audit records only its type.
	return fmt.Sprintf("SQL execution failed (%T); inspect command logs", err)
}

func (s *session) auditFailure(version int64, direction string, cause error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.event(ctx, version, direction, "failed", failureNote(cause))
}

func (s *session) History(ctx context.Context) ([]migration.Record, error) {
	rows, err := s.conn.QueryContext(ctx, "SELECT version,name,batch,executed_at,execution_time,checksum,dirty FROM graft_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var history []migration.Record
	for rows.Next() {
		var r migration.Record
		var dirty string
		if err := rows.Scan(&r.Version, &r.Name, &r.Batch, &r.ExecutedAt, &r.ExecutionTime, &r.Checksum, &dirty); err != nil {
			return nil, err
		}
		if dirty != "" {
			return nil, &DirtyError{Version: r.Version, Direction: dirty}
		}
		history = append(history, r)
	}
	return history, rows.Err()
}

func changedOne(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("mysql migration history changed unexpectedly")
	}
	return nil
}

func (s *session) Apply(ctx context.Context, m migration.Migration, batch int) (migration.Record, error) {
	r := migration.Record{Version: m.Version, Name: m.Name, Batch: batch, Checksum: m.Checksum, ExecutedAt: time.Now().UTC()}
	if m.Dialect != "mysql" {
		return r, errors.New("mysql: load migrations using migration.LoadDialect with dialect mysql")
	}
	if _, err := s.History(ctx); err != nil {
		return r, err
	}
	_, err := s.conn.ExecContext(ctx, "INSERT INTO graft_migrations (version,name,batch,executed_at,execution_time,checksum,dirty) VALUES (?,?,?,?,?,?,'up')", r.Version, r.Name, r.Batch, r.ExecutedAt, 0, r.Checksum)
	if err != nil {
		return r, err
	}
	if err = s.event(ctx, m.Version, "up", "started", ""); err != nil {
		return r, errors.Join(&DirtyError{m.Version, "up"}, err)
	}
	start := time.Now()
	if _, err = s.conn.ExecContext(ctx, m.Up); err != nil {
		s.auditFailure(m.Version, "up", err)
		return r, errors.Join(&DirtyError{m.Version, "up"}, err)
	}
	r.ExecutedAt, r.ExecutionTime = time.Now().UTC(), time.Since(start)
	if err = s.event(ctx, m.Version, "up", "sql_done", ""); err != nil {
		return r, errors.Join(&DirtyError{m.Version, "up"}, err)
	}
	err = changedOne(s.conn.ExecContext(ctx, "UPDATE graft_migrations SET dirty='',executed_at=?,execution_time=? WHERE version=? AND checksum=? AND dirty='up'", r.ExecutedAt, int64(r.ExecutionTime), r.Version, r.Checksum))
	if err != nil {
		return r, errors.Join(&DirtyError{m.Version, "up"}, err)
	}
	return r, nil
}

func (s *session) Revert(ctx context.Context, m migration.Migration) error {
	if m.Dialect != "mysql" {
		return errors.New("mysql: load migrations using migration.LoadDialect with dialect mysql")
	}
	if _, err := s.History(ctx); err != nil {
		return err
	}
	if err := changedOne(s.conn.ExecContext(ctx, "UPDATE graft_migrations SET dirty='down' WHERE version=? AND checksum=? AND dirty=''", m.Version, m.Checksum)); err != nil {
		return err
	}
	if err := s.event(ctx, m.Version, "down", "started", ""); err != nil {
		return errors.Join(&DirtyError{m.Version, "down"}, err)
	}
	if _, err := s.conn.ExecContext(ctx, m.Down); err != nil {
		s.auditFailure(m.Version, "down", err)
		return errors.Join(&DirtyError{m.Version, "down"}, err)
	}
	if err := s.event(ctx, m.Version, "down", "sql_done", ""); err != nil {
		return errors.Join(&DirtyError{m.Version, "down"}, err)
	}
	if err := changedOne(s.conn.ExecContext(ctx, "DELETE FROM graft_migrations WHERE version=? AND checksum=? AND dirty='down'", m.Version, m.Checksum)); err != nil {
		return errors.Join(&DirtyError{m.Version, "down"}, err)
	}
	return nil
}
