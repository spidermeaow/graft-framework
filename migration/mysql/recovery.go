package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spidermeaow/graft-framework/migration"
)

// DirtyMigration is a history row whose SQL outcome needs operator inspection.
type DirtyMigration struct {
	migration.Record
	Direction   string
	LastEvent   string
	LastEventAt time.Time
}

// Inspect returns dirty records under the same lock used by normal migrations.
func (s *Store) Inspect(ctx context.Context) ([]DirtyMigration, error) {
	var result []DirtyMigration
	err := s.withLock(ctx, false, func(locked migration.Session) error {
		var historyExists bool
		if err := locked.(*session).conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='graft_migrations')").Scan(&historyExists); err != nil {
			return err
		}
		if !historyExists {
			return nil
		}
		var eventsExist bool
		if err := locked.(*session).conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='graft_migration_events')").Scan(&eventsExist); err != nil {
			return err
		}
		rows, err := locked.(*session).conn.QueryContext(ctx, "SELECT version,name,batch,executed_at,execution_time,checksum,dirty FROM graft_migrations WHERE dirty<>'' ORDER BY version")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d DirtyMigration
			if err := rows.Scan(&d.Version, &d.Name, &d.Batch, &d.ExecutedAt, &d.ExecutionTime, &d.Checksum, &d.Direction); err != nil {
				return err
			}
			result = append(result, d)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for i := range result {
			if !eventsExist {
				break
			}
			var event sql.NullString
			var at sql.NullTime
			err := locked.(*session).conn.QueryRowContext(ctx, "SELECT event,created_at FROM graft_migration_events WHERE version=? ORDER BY id DESC LIMIT 1", result[i].Version).Scan(&event, &at)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if event.Valid {
				result[i].LastEvent = event.String
			}
			if at.Valid {
				result[i].LastEventAt = at.Time
			}
		}
		return nil
	})
	return result, err
}

// RepairPlan verifies the exact local migration against the dirty history row.
// It does not change history. The caller must separately verify actual schema/data.
func (s *Store) RepairPlan(ctx context.Context, local migration.Migration, target string) (DirtyMigration, error) {
	var dirty DirtyMigration
	err := s.withLock(ctx, false, func(locked migration.Session) error {
		var err error
		dirty, err = checkedDirty(ctx, locked.(*session).conn, local, target)
		return err
	})
	return dirty, err
}

func checkedDirty(ctx context.Context, conn *sql.Conn, local migration.Migration, target string) (DirtyMigration, error) {
	if target != "pending" && target != "applied" {
		return DirtyMigration{}, errors.New("repair target must be pending or applied")
	}
	if local.Dialect != "mysql" || local.Version <= 0 || local.Name == "" || len(local.Checksum) != 64 {
		return DirtyMigration{}, errors.New("repair requires a loaded MySQL migration")
	}
	var d DirtyMigration
	err := conn.QueryRowContext(ctx, "SELECT version,name,batch,executed_at,execution_time,checksum,dirty FROM graft_migrations WHERE version=?", local.Version).Scan(&d.Version, &d.Name, &d.Batch, &d.ExecutedAt, &d.ExecutionTime, &d.Checksum, &d.Direction)
	if errors.Is(err, sql.ErrNoRows) {
		return d, fmt.Errorf("migration %d has no history row", local.Version)
	}
	if err != nil {
		return d, err
	}
	if d.Name != local.Name || d.Checksum != local.Checksum {
		return d, fmt.Errorf("migration %d name/checksum does not match local SQL; repair refused", local.Version)
	}
	if d.Direction != "up" && d.Direction != "down" {
		return d, fmt.Errorf("migration %d is not dirty", local.Version)
	}
	return d, nil
}

// Repair changes only a checked dirty history row and records an audit event in
// one InnoDB transaction. It cannot verify schema/data restoration; the operator
// confirms that separately after using RepairPlan.
func (s *Store) Repair(ctx context.Context, local migration.Migration, target, note string) error {
	note = strings.TrimSpace(note)
	if note == "" || len(note) > 980 {
		return errors.New("repair requires an operator note of 1-980 characters")
	}
	return s.WithLock(ctx, func(locked migration.Session) error {
		conn := locked.(*session).conn
		d, err := checkedDirty(ctx, conn, local, target)
		if err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if target == "pending" {
			err = changedOne(tx.ExecContext(ctx, "DELETE FROM graft_migrations WHERE version=? AND name=? AND checksum=? AND dirty=?", local.Version, local.Name, local.Checksum, d.Direction))
		} else {
			err = changedOne(tx.ExecContext(ctx, "UPDATE graft_migrations SET dirty='' WHERE version=? AND name=? AND checksum=? AND dirty=?", local.Version, local.Name, local.Checksum, d.Direction))
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO graft_migration_events (version,direction,event,created_at,note) VALUES (?,?,?,?,?)", local.Version, d.Direction, "repaired", time.Now().UTC(), "mark-"+target+": "+note)
		if err != nil {
			return err
		}
		return tx.Commit()
	})
}
