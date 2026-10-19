package main

import (
	"context"
	"errors"
	"fmt"
	"leaguewatcher/internal/leaguewatcher/db"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// advisoryLockKey is the bigint key used for pg_advisory_lock so two
// concurrently running migrators fail fast instead of racing. Chosen
// to be a stable identifier ("migra" as ASCII).
const advisoryLockKey int64 = 0x6D69677261

// sentinel errors mapped to specific exit codes by main().
var (
	ErrLockHeld     = errors.New("advisory lock held by another process")
	ErrNoMigrations = errors.New("no migrations known to binary")
)

// appliedRow is one row of schema_migrations.
type appliedRow struct {
	Version   int
	Name      string
	AppliedAt time.Time
}

// direction selects between up (apply) and down (rollback).
type direction int

const (
	dirUp direction = iota
	dirDown
)

// dbHandle is the interface the runner uses to talk to Postgres. Real
// implementation uses pgxpool; tests substitute a fake.
type dbHandle interface {
	// TryLock attempts to take the runner-wide advisory lock. Returns
	// ErrLockHeld if another session holds it. Caller must later call
	// Unlock on the returned handle.
	TryLock(ctx context.Context) (unlock func(), err error)

	// ReadApplied returns every row in schema_migrations, keyed by
	// version. Returns an empty map (not an error) when the table does
	// not exist yet (fresh database).
	ReadApplied(ctx context.Context) (map[int]appliedRow, error)

	// ApplyMigration executes the migration's SQL and updates the
	// bookkeeping row inside a single transaction.
	ApplyMigration(ctx context.Context, m db.Migration, dir direction) error

	// Close releases the underlying connection.
	Close()
}

// pgHandle is the production dbHandle.
type pgHandle struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
	// lockConn is a dedicated connection used to hold the advisory lock
	// so the lock is not tied to a single pool slot.
	lockConn *pgx.Conn
}

func newPGHandle(ctx context.Context, url string, logger *slog.Logger) (*pgHandle, error) {
	pool, err := db.Connect(ctx, url, logger.With("component", "migrator-db"))
	if err != nil {
		return nil, err
	}
	return &pgHandle{pool: pool, logger: logger}, nil
}

func (h *pgHandle) TryLock(ctx context.Context) (func(), error) {
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire lock connection: %w", err)
	}

	var got bool
	if err := conn.Conn().QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockKey).Scan(&got); err != nil {
		conn.Release()
		return nil, fmt.Errorf("try advisory lock: %w", err)
	}
	if !got {
		conn.Release()
		return nil, ErrLockHeld
	}

	h.lockConn = conn.Conn()
	unlock := func() {
		ctx := context.Background()
		_, _ = h.lockConn.Exec(ctx, "SELECT pg_advisory_unlock($1)", advisoryLockKey)
		conn.Release()
	}
	return unlock, nil
}

func (h *pgHandle) ReadApplied(ctx context.Context) (map[int]appliedRow, error) {
	var exists bool
	if err := h.pool.QueryRow(ctx, `SELECT to_regclass('public.schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check schema_migrations existence: %w", err)
	}
	if !exists {
		return map[int]appliedRow{}, nil
	}

	rows, err := h.pool.Query(ctx, `SELECT version, name, applied_at FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	out := map[int]appliedRow{}
	for rows.Next() {
		var r appliedRow
		if err := rows.Scan(&r.Version, &r.Name, &r.AppliedAt); err != nil {
			return nil, fmt.Errorf("scan applied row: %w", err)
		}
		out[r.Version] = r
	}
	return out, rows.Err()
}

func (h *pgHandle) ApplyMigration(ctx context.Context, m db.Migration, dir direction) error {
	var sqlBody, bookkeepSQL string
	switch dir {
	case dirUp:
		sqlBody = m.UpSQL
		bookkeepSQL = `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`
	case dirDown:
		sqlBody = m.DownSQL
		bookkeepSQL = `DELETE FROM schema_migrations WHERE version = $1`
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration txn: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Cheap safety net: refuse to wait on a table lock indefinitely.
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
		return fmt.Errorf("set lock_timeout: %w", err)
	}

	if sqlBody != "" {
		tag, err := tx.Exec(ctx, sqlBody)
		if err != nil {
			return fmt.Errorf("migration %d (%s) %s failed: %w", m.Version, m.Name, dirString(dir), err)
		}
		h.logger.Info("migration statement executed",
			slog.Int("version", m.Version),
			slog.String("name", m.Name),
			slog.String("direction", dirString(dir)),
			slog.String("tag", tag.String()),
		)
	}

	var bkErr error
	if dir == dirUp {
		_, bkErr = tx.Exec(ctx, bookkeepSQL, m.Version, m.Name)
	} else {
		_, bkErr = tx.Exec(ctx, bookkeepSQL, m.Version)
	}
	if bkErr != nil {
		return fmt.Errorf("migration %d bookkeeping failed: %w", m.Version, bkErr)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %d: %w", m.Version, err)
	}
	return nil
}

func (h *pgHandle) Close() { h.pool.Close() }

// planUp returns the migrations not yet applied, in ascending order,
// capped at target (target <= 0 means "all pending").
func planUp(all []db.Migration, applied map[int]appliedRow, target int) []db.Migration {
	var pending []db.Migration
	for _, m := range all {
		if _, ok := applied[m.Version]; !ok {
			if target > 0 && m.Version > target {
				continue
			}
			pending = append(pending, m)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Version < pending[j].Version })
	return pending
}

// planDown returns the migrations to roll back, in descending order.
// count <= 0 means "roll back one" (the default), so callers should pass
// 1 explicitly when they want that behavior; this function simply takes
// the highest-N applied versions that have files.
func planDown(all []db.Migration, applied map[int]appliedRow, count int) []db.Migration {
	if count <= 0 {
		count = 1
	}
	byVersion := make(map[int]db.Migration, len(all))
	for _, m := range all {
		byVersion[m.Version] = m
	}
	appliedVersions := make([]int, 0, len(applied))
	for v := range applied {
		appliedVersions = append(appliedVersions, v)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(appliedVersions)))

	var out []db.Migration
	for _, v := range appliedVersions {
		if len(out) >= count {
			break
		}
		m, ok := byVersion[v]
		if !ok {
			// Database is ahead of the binary: refuse to silently skip.
			continue
		}
		out = append(out, m)
	}
	return out
}

func dirString(d direction) string {
	if d == dirUp {
		return "up"
	}
	return "down"
}

// statusReport is a one-shot summary used by both `status` and `up/down`.
type statusReport struct {
	Applied []appliedRow
	Pending []db.Migration
}

// readStatus gathers the applied set and computes what is still pending.
func readStatus(ctx context.Context, h dbHandle, all []db.Migration) (statusReport, error) {
	applied, err := h.ReadApplied(ctx)
	if err != nil {
		return statusReport{}, err
	}
	rep := statusReport{
		Applied: make([]appliedRow, 0, len(applied)),
	}
	for _, r := range applied {
		rep.Applied = append(rep.Applied, r)
	}
	sort.Slice(rep.Applied, func(i, j int) bool { return rep.Applied[i].Version < rep.Applied[j].Version })
	rep.Pending = planUp(all, applied, 0)
	return rep, nil
}

// formatStatus renders a statusReport for stderr.
func formatStatus(rep statusReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "applied: %d\n", len(rep.Applied))
	for _, r := range rep.Applied {
		fmt.Fprintf(&sb, "  %04d  %-32s  %s\n", r.Version, r.Name, r.AppliedAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(&sb, "pending: %d\n", len(rep.Pending))
	for _, m := range rep.Pending {
		fmt.Fprintf(&sb, "  %04d  %s\n", m.Version, m.Name)
	}
	return sb.String()
}

// applyPlan executes the given plan against the database. Returns when
// the plan is fully applied or on the first failure.
func applyPlan(ctx context.Context, h dbHandle, plan []db.Migration, dir direction, logger *slog.Logger) error {
	if len(plan) == 0 {
		logger.Info("nothing to do")
		return nil
	}

	unlock, err := h.TryLock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	for _, m := range plan {
		if err := h.ApplyMigration(ctx, m, dir); err != nil {
			return err
		}
		logger.Info("migration applied",
			slog.Int("version", m.Version),
			slog.String("name", m.Name),
			slog.String("direction", dirString(dir)),
		)
	}
	return nil
}
