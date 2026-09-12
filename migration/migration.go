// Package migration runs ordered SQL-first migrations through a transactional Store.
package migration

import (
	"context"
	"time"
)

// Migration is an immutable SQL file parsed by Load.
type Migration struct {
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

// Session is valid only inside WithLock. Apply and Revert must atomically execute
// SQL and insert/delete its history row in the same transaction.
type Session interface {
	History(context.Context) ([]Record, error)
	Apply(context.Context, Migration, int) (Record, error)
	Revert(context.Context, Migration) error
}
