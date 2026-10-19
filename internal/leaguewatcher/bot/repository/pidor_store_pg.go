// Postgres-backed PidorStore implementation.
//
// The schema is migration 0002_pidor_rolls (ADR-011):
//
//	CREATE TABLE pidor_rolls (
//	    id           BIGSERIAL PRIMARY KEY,
//	    channel_id   TEXT NOT NULL,
//	    user_id      TEXT NOT NULL,
//	    user_name    TEXT NOT NULL,
//	    rolled_on    DATE NOT NULL,
//	    rolled_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
//	    times_rolled INTEGER NOT NULL DEFAULT 1 CHECK (times_rolled > 0),
//	    UNIQUE (channel_id, rolled_on)
//	);
//
// The UNIQUE constraint is what makes TryChooseToday atomic: two
// concurrent invocations cannot both successfully INSERT for the same
// (channel_id, rolled_on); the second one is silently dropped by
// ON CONFLICT DO NOTHING and reports rows_affected=0.
//
// For tests, the table name can be swapped via WithPidorTable —
// integration tests use a mirror table (see ADR-011 §"Backfill
// strategy" tests and tools/pidors-backfill/integration_test.go for
// the same safety pattern).
package repository

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PidorStorePostgres is the production PidorStore.
type PidorStorePostgres struct {
	pool  *pgxpool.Pool
	table string
	now   func() time.Time // injectable for time-travel tests
}

// PidorStoreOption is a functional option for configuring
// PidorStorePostgres.
type PidorStoreOption func(*PidorStorePostgres)

// pidorTableNamePattern matches what we allow to interpolate into
// SQL — defence in depth against accidentally passing user input.
var pidorTableNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// WithPidorTable overrides the target table name. Used by integration
// tests to run against a mirror table; production callers should not
// set this option.
func WithPidorTable(name string) PidorStoreOption {
	return func(s *PidorStorePostgres) { s.table = name }
}

// withClock injects a fake clock. Test-only; production callers should
// not set this option. Not exported because tests live in the same
// package.
func withClock(now func() time.Time) PidorStoreOption {
	return func(s *PidorStorePostgres) { s.now = now }
}

// NewPidorStorePostgres constructs a Postgres-backed store. Returns
// an error if the pool is nil — silently dropping pidor commands is
// worse than failing bot startup.
func NewPidorStorePostgres(pool *pgxpool.Pool, opts ...PidorStoreOption) (*PidorStorePostgres, error) {
	if pool == nil {
		return nil, errors.New("pgxpool.Pool is required")
	}
	s := &PidorStorePostgres{
		pool:  pool,
		table: "pidor_rolls",
		now:   time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	if !pidorTableNamePattern.MatchString(s.table) {
		return nil, fmt.Errorf("pidor table name %q is not a safe identifier", s.table)
	}
	return s, nil
}

// CanBeChosen reports whether the channel has not yet had a pidor roll
// for the given day.
func (s *PidorStorePostgres) CanBeChosen(ctx context.Context, channelID string, today time.Time) (bool, error) {
	q := fmt.Sprintf(`SELECT EXISTS(
	  SELECT 1 FROM %s
	   WHERE channel_id = $1 AND rolled_on = $2::date
	)`, s.table)
	var taken bool
	if err := s.pool.QueryRow(ctx, q, channelID, today.Format("2006-01-02")).Scan(&taken); err != nil {
		return false, fmt.Errorf("can-be-chosen query: %w", err)
	}
	return !taken, nil
}

// TryChooseToday atomically reserves today's pidor slot for this channel
// with the given winner. See the interface godoc for the return contract.
func (s *PidorStorePostgres) TryChooseToday(
	ctx context.Context,
	channelID, userID, userName string,
	today time.Time,
) (bool, error) {
	q := fmt.Sprintf(`
INSERT INTO %s (channel_id, user_id, user_name, rolled_on, times_rolled)
VALUES ($1, $2, $3, $4::date, 1)
ON CONFLICT (channel_id, rolled_on) DO NOTHING
RETURNING id`, s.table)
	var id int64
	err := s.pool.QueryRow(ctx, q,
		channelID, userID, userName, today.Format("2006-01-02")).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// ON CONFLICT DO NOTHING fired: another invocation took
			// today's slot. Not an error from our perspective.
			return false, nil
		}
		return false, fmt.Errorf("insert roll: %w", err)
	}
	return true, nil
}

// Stats returns lifetime aggregates for the channel keyed by user_id.
// The "Name" value is the most recent user_name snapshot for that user
// in this channel — users who rename will surface with their current
// name here, matching the legacy JSON store's semantics.
func (s *PidorStorePostgres) Stats(ctx context.Context, channelID string) (map[string]PidorStat, error) {
	// NB: we correlate the inner subquery on r.user_id (in GROUP BY) plus
	// the literal $1 channel — not on r.channel_id (not grouped, would
	// fail with SQLSTATE 42803).
	q := fmt.Sprintf(`
SELECT r.user_id,
       (SELECT r2.user_name FROM %s r2
         WHERE r2.channel_id = $1 AND r2.user_id = r.user_id
         ORDER BY r2.rolled_on DESC, r2.id DESC LIMIT 1) AS latest_name,
       SUM(r.times_rolled) AS wins
  FROM %s r
 WHERE r.channel_id = $1
 GROUP BY r.user_id`, s.table, s.table)
	rows, err := s.pool.Query(ctx, q, channelID)
	if err != nil {
		return nil, fmt.Errorf("stats query: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]PidorStat)
	for rows.Next() {
		var userID, userName string
		var wins int
		if err := rows.Scan(&userID, &userName, &wins); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		stats[userID] = PidorStat{Name: userName, Count: wins}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return stats, nil
}
