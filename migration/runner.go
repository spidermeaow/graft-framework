package migration

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// Runner executes a validated migration set. A nil Logger uses slog.Default.
type Runner struct {
	Store  Store
	Logger *slog.Logger
}

func (r Runner) log() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

func validate(migrations []Migration, history []Record) ([]Migration, error) {
	ordered := append([]Migration(nil), migrations...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Version < ordered[j].Version })
	local := map[int64]Migration{}
	for _, m := range ordered {
		if m.Version <= 0 || m.Name == "" || len(m.Checksum) != 64 {
			return nil, fmt.Errorf("invalid migration %d; use migration.Load", m.Version)
		}
		if _, ok := local[m.Version]; ok {
			return nil, fmt.Errorf("duplicate migration version %d", m.Version)
		}
		if err := validateSQL(m.Up); err != nil {
			return nil, err
		}
		if err := validateSQL(m.Down); err != nil {
			return nil, err
		}
		local[m.Version] = m
	}
	applied := map[int64]bool{}
	var max int64
	for _, h := range history {
		m, ok := local[h.Version]
		if !ok {
			return nil, fmt.Errorf("applied migration %d is missing locally", h.Version)
		}
		if applied[h.Version] {
			return nil, fmt.Errorf("duplicate history version %d", h.Version)
		}
		if m.Checksum != h.Checksum || m.Name != h.Name {
			return nil, fmt.Errorf("checksum/name mismatch for applied migration %d", h.Version)
		}
		if h.Batch <= 0 {
			return nil, fmt.Errorf("invalid batch for migration %d", h.Version)
		}
		applied[h.Version] = true
		if h.Version > max {
			max = h.Version
		}
	}
	for _, m := range ordered {
		if !applied[m.Version] && m.Version < max {
			return nil, fmt.Errorf("out-of-order migration %d precedes an applied migration", m.Version)
		}
	}
	return ordered, nil
}

// Up applies pending migrations in one new batch. Each migration commits separately;
// on failure earlier successful migrations remain recorded and retries are safe.
func (r Runner) Up(ctx context.Context, migrations []Migration) error {
	if r.Store == nil {
		return fmt.Errorf("migration: nil store")
	}
	return r.Store.WithLock(ctx, func(s Session) error {
		history, err := s.History(ctx)
		if err != nil {
			return err
		}
		ordered, err := validate(migrations, history)
		if err != nil {
			return err
		}
		batch := 1
		applied := map[int64]bool{}
		for _, h := range history {
			applied[h.Version] = true
			if h.Batch >= batch {
				batch = h.Batch + 1
			}
		}
		for _, m := range ordered {
			if applied[m.Version] {
				continue
			}
			record, err := s.Apply(ctx, m, batch)
			if err != nil {
				return fmt.Errorf("apply %d_%s: %w", m.Version, m.Name, err)
			}
			r.log().InfoContext(ctx, "migration applied", "version", m.Version, "name", m.Name, "batch", batch, "duration", record.ExecutionTime)
		}
		return nil
	})
}

// Status returns all migrations in ascending version order and detects history drift.
func (r Runner) Status(ctx context.Context, migrations []Migration) ([]Status, error) {
	if r.Store == nil {
		return nil, fmt.Errorf("migration: nil store")
	}
	var result []Status
	err := r.Store.WithLock(ctx, func(s Session) error {
		history, err := s.History(ctx)
		if err != nil {
			return err
		}
		ordered, err := validate(migrations, history)
		if err != nil {
			return err
		}
		byVersion := map[int64]Record{}
		for _, h := range history {
			byVersion[h.Version] = h
		}
		for _, m := range ordered {
			status := Status{Migration: m}
			if h, ok := byVersion[m.Version]; ok {
				status.Applied = &h
			}
			result = append(result, status)
		}
		return nil
	})
	return result, err
}

// Rollback reverses the latest batch when steps is zero, or the latest N individual
// migrations when positive. Applied records are removed only after successful Down SQL.
func (r Runner) Rollback(ctx context.Context, migrations []Migration, steps int) error {
	if steps < 0 {
		return fmt.Errorf("rollback steps must not be negative")
	}
	if r.Store == nil {
		return fmt.Errorf("migration: nil store")
	}
	return r.Store.WithLock(ctx, func(s Session) error {
		history, err := s.History(ctx)
		if err != nil {
			return err
		}
		ordered, err := validate(migrations, history)
		if err != nil {
			return err
		}
		byVersion := map[int64]Migration{}
		for _, m := range ordered {
			byVersion[m.Version] = m
		}
		sort.Slice(history, func(i, j int) bool { return history[i].Version > history[j].Version })
		batch := 0
		for _, h := range history {
			if h.Batch > batch {
				batch = h.Batch
			}
		}
		count := 0
		for _, h := range history {
			if steps == 0 && h.Batch != batch {
				continue
			}
			if steps > 0 && count >= steps {
				break
			}
			m := byVersion[h.Version]
			start := time.Now()
			if err := s.Revert(ctx, m); err != nil {
				return fmt.Errorf("revert %d_%s: %w", m.Version, m.Name, err)
			}
			r.log().InfoContext(ctx, "migration reverted", "version", m.Version, "name", m.Name, "duration", time.Since(start))
			count++
		}
		return nil
	})
}
