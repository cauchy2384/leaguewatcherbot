// Backfill planning and application for pidors.json → pidor_rolls.
//
// The algorithm is described in ADR-011 §"Backfill strategy":
//
//	Within a channel, sort user_id values lexicographically ascending.
//	The i-th user (0-indexed) gets rolled_on = DATE '1970-01-01' + i days,
//	and times_rolled = the lifetime count from pidors.json.
//
// Because date assignment is deterministic across runs and the schema
// has UNIQUE (channel_id, rolled_on), re-running the backfill yields
// INSERT ... ON CONFLICT DO NOTHING → 0 rows affected for already-
// present rows. The runner reports "skipped" for those and "inserted"
// for newly added rows, making reruns safe and visible.
package main

import (
	"context"
	"fmt"
	"leaguewatcher/internal/leaguewatcher/bot/repository"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// syntheticEpoch is the base date for backfilled rows. Any row with
// rolled_on in this era is unambiguously not a live roll.
var syntheticEpoch = time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC)

// planRow is a computed but unwritten pidor_rolls row.
type planRow struct {
	ChannelID   string
	UserID      string
	UserName    string
	RolledOn    time.Time
	TimesRolled int
}

// computePlan converts pidors.json Stats into pidor_rolls rows.
// Returns the rows to insert (in deterministic order) plus warnings
// for entries that were skipped (bad input). Warnings do NOT cause
// the run to fail — the choice is to import what's valid rather than
// abort on a single corrupt entry.
func computePlan(p *repository.Pidor) ([]planRow, []string) {
	if p == nil || len(p.Stats) == 0 {
		return nil, nil
	}

	var rows []planRow
	var warnings []string

	// Iterate channels in deterministic order for stable output and
	// consistent test expectations.
	channelIDs := make([]string, 0, len(p.Stats))
	for ch := range p.Stats {
		channelIDs = append(channelIDs, ch)
	}
	sort.Strings(channelIDs)

	for _, channelID := range channelIDs {
		stats := p.Stats[channelID]

		// Filter + sort user IDs. Lexicographic sort is deliberate:
		// gives the same date assignment on every run regardless of
		// iteration order or map shuffling.
		type userEntry struct {
			id    string
			name  string
			count int
		}
		users := make([]userEntry, 0, len(stats))
		for userID, s := range stats {
			if s.Count <= 0 {
				warnings = append(warnings,
					fmt.Sprintf("channel=%s user=%s: skipped (count=%d ≤ 0)",
						channelID, userID, s.Count))
				continue
			}
			if s.Name == "" {
				warnings = append(warnings,
					fmt.Sprintf("channel=%s user=%s: skipped (empty name)",
						channelID, userID))
				continue
			}
			users = append(users, userEntry{id: userID, name: s.Name, count: s.Count})
		}
		sort.Slice(users, func(i, j int) bool { return users[i].id < users[j].id })

		for i, u := range users {
			rows = append(rows, planRow{
				ChannelID:   channelID,
				UserID:      u.id,
				UserName:    u.name,
				RolledOn:    syntheticEpoch.AddDate(0, 0, i),
				TimesRolled: u.count,
			})
		}
	}

	return rows, warnings
}

const insertSQL = `
INSERT INTO pidor_rolls (channel_id, user_id, user_name, rolled_on, times_rolled)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (channel_id, rolled_on) DO NOTHING
`

// applyPlan inserts all rows in a single transaction. Returns the
// number of rows actually inserted (updates.RowsAffected = 1) and
// the number skipped due to unique-violation conflict (0).
//
// On error the transaction is rolled back; no partial backfill state
// is left behind.
func applyPlan(ctx context.Context, pool *pgxpool.Pool, rows []planRow) (inserted int, skipped int, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	for _, r := range rows {
		tag, err := tx.Exec(ctx, insertSQL,
			r.ChannelID, r.UserID, r.UserName, r.RolledOn, r.TimesRolled)
		if err != nil {
			return 0, 0, fmt.Errorf("insert channel=%s user=%s rolled_on=%s: %w",
				r.ChannelID, r.UserID, r.RolledOn.Format("2006-01-02"), err)
		}
		if tag.RowsAffected() == 0 {
			skipped++
		} else {
			inserted++
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("commit: %w", err)
	}
	return inserted, skipped, nil
}
