package repository

import (
	"context"
	"fmt"
	"leaguewatcher/internal/leaguewatcher/db"
	"leaguewatcher/internal/leaguewatcher/pidorjson"
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestPidorStorePostgresIntegration runs the Postgres-backed store
// against a real database. Skipped unless MIGRATOR_TEST_DSN is set
// (shared env var with the migrator / pidors-backfill integration
// tests — all represent "connect to a real Postgres for verification").
//
// ⚠️ SAFETY: this test only touches the isolated mirror table
// `pidor_rolls_itest`. The real `pidor_rolls` is never opened — the
// WithPidorTable option redirects every query.
//
// Run manually:
//
//	MIGRATOR_TEST_DSN="postgres://…" \
//	  go test ./internal/leaguewatcher/bot/repository/ \
//	    -run TestPidorStorePostgresIntegration -v
func TestPidorStorePostgresIntegration(t *testing.T) {
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
	defer pool.Close()

	table := fmt.Sprintf("pidor_rolls_itest_%d", time.Now().UnixNano())

	// Defers run LIFO: pool.Close registered FIRST so it executes LAST.
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		_, _ = pool.Exec(cctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table))
	}()

	// Mirror the real pidor_rolls shape INCLUDING the UNIQUE constraint —
	// we want TryChooseToday's ON CONFLICT behaviour to be genuinely
	// exercised.
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id           BIGSERIAL   PRIMARY KEY,
			channel_id   TEXT        NOT NULL,
			user_id      TEXT        NOT NULL,
			user_name    TEXT        NOT NULL,
			rolled_on    DATE        NOT NULL,
			rolled_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
			times_rolled INTEGER     NOT NULL DEFAULT 1 CHECK (times_rolled > 0),
			UNIQUE (channel_id, rolled_on)
		)`, table)); err != nil {
		t.Fatalf("create mirror table: %v", err)
	}

	store, err := NewPidorStorePostgres(pool, WithPidorTable(table))
	if err != nil {
		t.Fatalf("NewPidorStorePostgres: %v", err)
	}

	today := time.Date(2026, time.October, 19, 0, 0, 0, 0, time.UTC)

	// === CanBeChosen on an empty table → true ===
	ok, err := store.CanBeChosen(ctx, "chan-1", today)
	if err != nil {
		t.Fatalf("CanBeChosen (empty): %v", err)
	}
	if !ok {
		t.Error("CanBeChosen returned false on an empty table")
	}

	// === TryChooseToday on empty slot → true ===
	won, err := store.TryChooseToday(ctx, "chan-1", "u-1", "alice", today)
	if err != nil {
		t.Fatalf("TryChooseToday: %v", err)
	}
	if !won {
		t.Error("TryChooseToday returned false on empty slot")
	}

	// === Re-call TryChooseToday on the same channel + day → false ===
	won, err = store.TryChooseToday(ctx, "chan-1", "u-2", "bob", today)
	if err != nil {
		t.Fatalf("TryChooseToday (race): %v", err)
	}
	if won {
		t.Error("TryChooseToday returned true on taken slot — race guard failed")
	}

	// === Same day on a DIFFERENT channel → still available ===
	won, err = store.TryChooseToday(ctx, "chan-2", "u-2", "bob", today)
	if err != nil {
		t.Fatalf("TryChooseToday (different channel): %v", err)
	}
	if !won {
		t.Error("TryChooseToday returned false on a different channel — must be independent")
	}

	// === Tomorrow on chan-1 is still open ===
	tomorrow := today.Add(24 * time.Hour)
	ok, err = store.CanBeChosen(ctx, "chan-1", tomorrow)
	if err != nil {
		t.Fatalf("CanBeChosen (tomorrow): %v", err)
	}
	if !ok {
		t.Error("CanBeChosen returned false for tomorrow after today's roll")
	}

	// === Stats aggregates: pre-seed a backfill-like row, then take another
	// today's roll on a different channel, verify map shape ===
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (channel_id, user_id, user_name, rolled_on, times_rolled)
		VALUES ('chan-3', 'u-old', 'OldName', DATE '1970-01-01', 42)`, table)); err != nil {
		t.Fatalf("seed backfill row: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (channel_id, user_id, user_name, rolled_on, times_rolled)
		VALUES ('chan-3', 'u-old', 'OldName', DATE '1970-01-02', 5)`, table)); err != nil {
		t.Fatalf("seed backfill row 2: %v", err)
	}

	stats, err := store.Stats(ctx, "chan-3")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("Stats returned %d users, want 1", len(stats))
	}
	if got := stats["u-old"].Count; got != 47 {
		t.Errorf("Stats u-old.Count = %d, want 42+5 = 47", got)
	}
	if got := stats["u-old"].Name; got != "OldName" {
		t.Errorf("Stats u-old.Name = %q, want OldName", got)
	}
}

// TestStatsParityAgainstJSON loads a real pidors.json snapshot and
// asserts the Postgres Stats() aggregation matches the JSON state for
// every (channel, user) pair. **Pre-merge sign-off test** — see
// PLAN-pidor-db-consumer.md §6.
//
// ⚠️ THIS TEST READS THE REAL pidor_rolls TABLE (not the mirror),
// because its entire purpose is to verify the production data matches
// the source-of-truth JSON bundle.
//
// Required env vars:
//   - MIGRATOR_TEST_DSN: real Postgres connection (production OK).
//   - PIDORS_TEST_JSON_PATH: absolute path to pidors.json snapshot.
//
// Both must be set or the test skips.
func TestStatsParityAgainstJSON(t *testing.T) {
	dsn := os.Getenv("MIGRATOR_TEST_DSN")
	jsonPath := os.Getenv("PIDORS_TEST_JSON_PATH")
	if dsn == "" || jsonPath == "" {
		t.Skip("MIGRATOR_TEST_DSN and PIDORS_TEST_JSON_PATH must both be set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	pool, err := db.Connect(ctx, dsn, logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	// Load pidors.json snapshot using the shared legacy-format loader.
	legacy, err := pidorjson.Load(jsonPath)
	if err != nil {
		t.Fatalf("load pidors.json: %v", err)
	}
	t.Logf("loaded pidors.json: %d channels, %d called entries",
		len(legacy.Stats), len(legacy.Called))

	// Use the REAL table for parity — that's the whole point.
	store, err := NewPidorStorePostgres(pool)
	if err != nil {
		t.Fatalf("NewPidorStorePostgres: %v", err)
	}

	// Sanity: log Called map for context.
	for chid, ts := range legacy.Called {
		t.Logf("  pidors.json Called[%s] = %s", chid, ts.Format(time.RFC3339))
	}

	var (
		passChannels, failChannels int
		details                    []string
	)
	for channelID, jsonStats := range legacy.Stats {
		dbStats, err := store.Stats(ctx, channelID)
		if err != nil {
			failChannels++
			details = append(details, fmt.Sprintf("channel=%s stats query err=%v", channelID, err))
			continue
		}

		jsonRolls := 0
		for _, s := range jsonStats {
			jsonRolls += s.Count
		}
		dbRolls := 0
		for _, s := range dbStats {
			dbRolls += s.Count
		}

		if len(jsonStats) != len(dbStats) || jsonRolls != dbRolls {
			failChannels++
			details = append(details, fmt.Sprintf(
				"channel=%s size mismatch: json users=%d rolls=%d, db users=%d rolls=%d",
				channelID, len(jsonStats), jsonRolls, len(dbStats), dbRolls))
			continue
		}

		// Per-user count comparison.
		channelOK := true
		for userID, jsonStat := range jsonStats {
			dbStat, ok := dbStats[userID]
			if !ok {
				channelOK = false
				details = append(details, fmt.Sprintf(
					"channel=%s user=%s missing in db", channelID, userID))
				continue
			}
			if dbStat.Count != jsonStat.Count {
				channelOK = false
				details = append(details, fmt.Sprintf(
					"channel=%s user=%s count mismatch: json=%d db=%d",
					channelID, userID, jsonStat.Count, dbStat.Count))
			}
		}
		for userID := range dbStats {
			if _, ok := jsonStats[userID]; !ok {
				channelOK = false
				details = append(details, fmt.Sprintf(
					"channel=%s user=%s present in db but not in json", channelID, userID))
			}
		}

		if channelOK {
			passChannels++
			t.Logf("channel=%s parity ok (%d users, %d rolls)", channelID, len(jsonStats), jsonRolls)
		} else {
			failChannels++
		}
	}

	if failChannels != 0 {
		t.Errorf("parity FAIL: %d channels ok, %d failed", passChannels, failChannels)
		for _, d := range details {
			t.Logf("  %s", d)
		}
	}
}
