package main

import (
	"context"
	"leaguewatcher/internal/leaguewatcher/bot/repository"
	"leaguewatcher/internal/leaguewatcher/db"
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestBackfillIntegration runs the importer against a real Postgres
// database. Skipped unless MIGRATOR_TEST_DSN is set (shared env var
// with the migrator's integration test — both are "connect to a real
// dev Neon database" use cases).
//
// Run manually:
//
//	MIGRATOR_TEST_DSN="postgres://user:pass@host/db?sslmode=require" \
//	  go test ./tools/pidors-backfill -run TestBackfillIntegration -v
//
// The test uses a synthetic channel ID suffixed with "-itest" to
// distinguish its rows from real production data, and cleans them up
// before exit.
func TestBackfillIntegration(t *testing.T) {
	dsn := os.Getenv("MIGRATOR_TEST_DSN")
	if dsn == "" {
		t.Skip("MIGRATOR_TEST_DSN not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	pool, err := db.Connect(ctx, dsn, logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Defers run LIFO: pool.Close is registered FIRST so it executes LAST,
	// after the data cleanup registered below.
	defer pool.Close()

	channelID := "integration-test-channel"
	userIDs := []string{"itu-alice", "itu-bob", "itu-carol"}

	// Ensure clean slate for this test's channel.
	if _, err := pool.Exec(ctx,
		"DELETE FROM pidor_rolls WHERE channel_id=$1", channelID); err != nil {
		t.Fatalf("pre-test cleanup: %v", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx,
			"DELETE FROM pidor_rolls WHERE channel_id=$1", channelID); err != nil {
			t.Logf("post-test cleanup failed: %v", err)
		}
	}()

	p := &repository.Pidor{
		Stats: map[string]map[string]repository.PidorStat{
			channelID: {
				userIDs[0]: {Name: "Alice", Count: 4},
				userIDs[1]: {Name: "Bob", Count: 2},
				userIDs[2]: {Name: "Carol", Count: 6},
			},
		},
	}
	rows, warnings := computePlan(p)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	// First apply: everything inserts.
	inserted, skipped, err := applyPlan(ctx, pool, rows)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if inserted != 3 || skipped != 0 {
		t.Fatalf("first apply: inserted=%d skipped=%d, want 3/0", inserted, skipped)
	}

	// Second apply: idempotent skip.
	inserted, skipped, err = applyPlan(ctx, pool, rows)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if inserted != 0 || skipped != 3 {
		t.Fatalf("second apply: inserted=%d skipped=%d, want 0/3", inserted, skipped)
	}

	// Verify aggregates via the acceptance query from ADR-011.
	type agg struct {
		userID string
		total  int
	}
	rowsQ, err := pool.Query(ctx,
		`SELECT user_id, SUM(times_rolled) FROM pidor_rolls
		  WHERE channel_id=$1 GROUP BY user_id ORDER BY 2 DESC`, channelID)
	if err != nil {
		t.Fatalf("query aggregates: %v", err)
	}
	defer rowsQ.Close()
	var got []agg
	for rowsQ.Next() {
		var a agg
		if err := rowsQ.Scan(&a.userID, &a.total); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, a)
	}
	if err := rowsQ.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	want := []agg{
		{userID: "itu-carol", total: 6},
		{userID: "itu-alice", total: 4},
		{userID: "itu-bob", total: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("aggregate row count: got %d want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("aggregate row %d: got %+v want %+v", i, got[i], w)
		}
	}
}
