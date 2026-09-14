// Package migration runs ordered SQL-first migrations through a database Store.
package migration

import (
	"context"
	"time"
)

// Migration is an immutable SQL file parsed by Load.
type Migration struct {
	// Dialect selects SQL validation rules; empty means postgres.
	Dialect  string
	Version  int64
	Name     string
	Up       string
	Down     string
	Checksum string
}

// Record is a committed migration in the database history.
type Record struct {
	Version       int64
	Name          string
	Batch         int
	ExecutedAt    time.Time
	ExecutionTime time.Duration
	Checksum      string
}

// Status describes a local migration and its optional committed history record.
type Status struct {
	Migration Migration
	Applied   *Record
}

// Store serializes an entire operation on one database session. WithLock must
// release the lock even on errors/panics. Concurrent contenders may fail fast.
type Store interface {
	WithLock(context.Context, func(Session) error) error
}

// Session is valid only inside WithLock. Transactional stores atomically execute
// SQL and update history. Nontransactional stores must persist a dirty marker
// before execution and refuse further operations until a failure is repaired.
type Session interface {
	History(context.Context) ([]Record, error)
	Apply(context.Context, Migration, int) (Record, error)
	Revert(context.Context, Migration) error
}
