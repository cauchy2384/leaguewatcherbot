package main

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

// testTable is the name of the mirror table the integration test
// exercises instead of the real pidor_rolls. The mirror is created
// with the same column shape but deliberately no constraints, no
// index, no PK — sufficient to exercise the backup/truncate/insert
// flow without putting the real production table at risk.
const testTable = "pidor_rolls_itest"

// TestBackfillIntegration runs the importer against a real Postgres
// database. Skipped unless MIGRATOR_TEST_DSN is set (shared env var
// with the migrator's integration test — both are "connect to a real
// dev Neon database" use cases).
//
// ⚠️ SAFETY: this test only touches the isolated mirror table
// `pidor_rolls_itest` and the backup tables `pidor_rolls_itest_backup_*`.
// Production data in `pidor_rolls` is NEVER read, written, or
// truncated by this test. Even so, please point MIGRATOR_TEST_DSN
// at a dev Neon branch when feasible (cheaper, fully isolated).
//
// Flow:
//  1. Create the mirror table (drop first if leftover from a prior run).
//  2. Seed mirror with "previous state" rows.
//  3. Apply backfill plan with new rows.
//  4. Verify backup table holds previous contents and mirror holds only new.
//  5. Re-apply to verify replace-not-append semantics.
//
// Run manually:
//
//	MIGRATOR_TEST_DSN="postgres://user:pass@host/db?sslmode=require" \
//	  go test ./tools/pidors-backfill -run TestBackfillIntegration -v
func TestBackfillIntegration(t *testing.T) {
	dsn := os.Getenv("MIGRATOR_TEST_DSN")
	if dsn == "" {
		t.Skip("MIGRATOR_TEST_DSN not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	pool, err := db.Connect(ctx, dsn, logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Defers run LIFO: pool.Close registered FIRST so it executes LAST.
	defer pool.Close()

	channelID := "integration-test-channel"
	backup1 := fmt.Sprintf("pidor_rolls_itest_backup_a_%d", time.Now().UnixNano())
	backup2 := fmt.Sprintf("pidor_rolls_itest_backup_b_%d", time.Now().UnixNano())

	mustExec := func(q string, args ...interface{}) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("sql %q: %v", q, err)
		}
	}

	// Clean slate: drop any leftover mirror + backups from prior runs.
	mustExec(fmt.Sprintf("DROP TABLE IF EXISTS %s", testTable))
	mustExec(fmt.Sprintf("DROP TABLE IF EXISTS %s", backup1))
	mustExec(fmt.Sprintf("DROP TABLE IF EXISTS %s", backup2))

	// Post-test cleanup. Registered AFTER pool.Close so it runs BEFORE it.
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		_, _ = pool.Exec(cctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", testTable))
		_, _ = pool.Exec(cctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", backup1))
		_, _ = pool.Exec(cctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", backup2))
	}()

	// Create the mirror table. Lean DDL — only what applyPlan actually
	// touches. Constraints aren't needed because the test asserts on
	// row contents, not constraint behaviour.
	mustExec(fmt.Sprintf(`
		CREATE TABLE %s (
			id           BIGSERIAL   PRIMARY KEY,
			channel_id   TEXT        NOT NULL,
			user_id      TEXT        NOT NULL,
			user_name    TEXT        NOT NULL,
			rolled_on    DATE        NOT NULL,
			rolled_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
			times_rolled INTEGER     NOT NULL
		)`, testTable))

	// === Stage 1: seed mirror with "previous state" rows. ===
	prior := []planRow{
		{ChannelID: channelID, UserID: "itu-prior-1", UserName: "OldA",
			RolledOn: mustDate("1970-01-01"), TimesRolled: 9},
		{ChannelID: channelID, UserID: "itu-prior-2", UserName: "OldB",
			RolledOn: mustDate("1970-01-02"), TimesRolled: 4},
	}
	for _, r := range prior {
		mustExec(fmt.Sprintf(
			`INSERT INTO %s (channel_id, user_id, user_name, rolled_on, times_rolled)
			 VALUES ($1, $2, $3, $4, $5)`, testTable),
			r.ChannelID, r.UserID, r.UserName, r.RolledOn, r.TimesRolled)
	}

	// === Stage 2: apply backfill plan. Backup should capture prior rows. ===
	p := &pidorjson.Pidor{
		Stats: map[string]map[string]pidorjson.PidorStat{
			channelID: {
				"itu-alice": {Name: "Alice", Count: 4},
				"itu-bob":   {Name: "Bob", Count: 2},
				"itu-carol": {Name: "Carol", Count: 6},
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

	inserted, truncated, err := applyPlan(ctx, pool, testTable, backup1, rows)
	if err != nil {
		t.Fatalf("apply plan: %v", err)
	}
	if inserted != 3 {
		t.Errorf("inserted=%d, want 3", inserted)
	}
	if truncated != 2 {
		t.Errorf("truncated=%d, want 2 (the seeded prior rows)", truncated)
	}

	// Backup should hold the two seeded prior rows.
	var backupCount int
	if err := pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s", backup1)).
		Scan(&backupCount); err != nil {
		t.Fatalf("count backup table: %v", err)
	}
	if backupCount != 2 {
		t.Errorf("backup count=%d, want 2", backupCount)
	}

	// Mirror should hold the three new rows, no priors.
	var priorsInMirror int
	if err := pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*) FROM %s WHERE channel_id=$1 AND user_id LIKE 'itu-prior-%%'`,
		testTable), channelID).Scan(&priorsInMirror); err != nil {
		t.Fatalf("count priors in mirror: %v", err)
	}
	if priorsInMirror != 0 {
		t.Errorf("priors still present in mirror: %d, want 0", priorsInMirror)
	}

	// Aggregates match the JSON.
	type agg struct {
		userID string
		total  int
	}
	rowsQ, err := pool.Query(ctx, fmt.Sprintf(
		`SELECT user_id, SUM(times_rolled) FROM %s
		  WHERE channel_id=$1 GROUP BY user_id ORDER BY 2 DESC`, testTable), channelID)
	if err != nil {
		t.Fatalf("query aggregates: %v", err)
	}
	var got []agg
	for rowsQ.Next() {
		var a agg
		if err := rowsQ.Scan(&a.userID, &a.total); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, a)
	}
	rowsQ.Close()
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

	// === Stage 3: re-apply with same plan into a second backup. ===
	// Expectation: not additive — mirror has same 3 rows, not 6.
	inserted, truncated, err = applyPlan(ctx, pool, testTable, backup2, rows)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if inserted != 3 {
		t.Errorf("second apply inserted=%d, want 3", inserted)
	}
	if truncated != 3 {
		t.Errorf("second apply truncated=%d, want 3 (rows from run 1)", truncated)
	}

	// Second backup captures run 1's output.
	if err := pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s", backup2)).
		Scan(&backupCount); err != nil {
		t.Fatalf("count backup2: %v", err)
	}
	if backupCount != 3 {
		t.Errorf("backup2 count=%d, want 3", backupCount)
	}

	// Mirror still has 3 rows (not 6) — replace-not-append proven.
	var mirrorCount int
	if err := pool.QueryRow(ctx,
		fmt.Sprintf("SELECT count(*) FROM %s WHERE channel_id=$1", testTable),
		channelID).Scan(&mirrorCount); err != nil {
		t.Fatalf("count mirror: %v", err)
	}
	if mirrorCount != 3 {
		t.Errorf("mirror count=%d after re-apply, want 3", mirrorCount)
	}

	// Confirm the REAL pidor_rolls table was never touched.
	var realTableRows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pidor_rolls").Scan(&realTableRows); err != nil {
		t.Fatalf("count real pidor_rolls: %v", err)
	}
	// We don't assert a specific number here (prod may legitimately
	// have rows). The test simply documents that prod pidor_rolls is
	// reachable and exists; nothing in this test wrote to it.
	t.Logf("sanity: real pidor_rolls has %d rows (untouched by this test)", realTableRows)
}

// TestApplyPlanRejectsUnsafeBackupName is a unit test that doesn't
// need a real DB. Just verify that obviously-bad identifiers are
// rejected before any SQL runs.
func TestApplyPlanRejectsUnsafeBackupName(t *testing.T) {
	t.Parallel()
	bad := []string{
		"",
		"pidor_rolls; DROP TABLE users;--",
		"\"quoted\"",
		"pidor rolls",
		"0startwithdigit",
		"UPPERCASE",
	}
	for _, name := range bad {
		if safeIdentifierPattern.MatchString(name) {
			t.Errorf("name %q unexpectedly matched safe pattern", name)
		}
	}
	good := []string{
		"pidor_rolls_backup_20261019_225600_utc",
		"a",
		"pidor_rolls_itest_backup_a_1729872000000000",
	}
	for _, name := range good {
		if !safeIdentifierPattern.MatchString(name) {
			t.Errorf("name %q unexpectedly rejected by safe pattern", name)
		}
	}
}
