// PidorStore abstracts pidor roll persistence. After the Postgres
// cutover (ADR-012), the only production implementation is
// PidorStorePostgres (see pidor_store_pg.go). The interface exists
// so bot handlers can be tested against an in-memory fake without a
// real database.
package repository

import (
	"context"
	"time"
)

// PidorStat is the lifetime aggregate for one user in one channel.
type PidorStat struct {
	Name  string
	Count int
}

// PidorStore abstracts pidor roll persistence and queries.
type PidorStore interface {
	// CanBeChosen reports whether a pidor may be chosen today for the
	// given channel on the supplied date. The caller supplies the
	// "current day" so the caller controls timezone semantics
	// (per ADR-011: Europe/Moscow).
	CanBeChosen(ctx context.Context, channelID string, today time.Time) (bool, error)

	// TryChooseToday atomically reserves today's pidor slot for this
	// channel with the given winner.
	//
	// Returns:
	//   (true, nil)  — this call won the slot; the row was inserted.
	//   (false, nil) — today's slot was already taken by a concurrent run.
	//   (false, err) — storage error; the bot surfaces a degradation message.
	//
	// The DB-level UNIQUE(channel_id, rolled_on) enforces atomicity
	// across any number of concurrent invocations.
	TryChooseToday(ctx context.Context, channelID, userID, userName string, today time.Time) (bool, error)

	// Stats returns the lifetime rollup for the channel, keyed by user
	// ID. The map shape mirrors the legacy pidors.json schema for
	// seamless consumption by the existing renderer code.
	Stats(ctx context.Context, channelID string) (map[string]PidorStat, error)
}
