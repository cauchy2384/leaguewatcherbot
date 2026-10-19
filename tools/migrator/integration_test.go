package main

import (
	"context"
	"leaguewatcher/internal/leaguewatcher/db"
	"os"
	"testing"
	"time"
)

// TestMigratorIntegration exercises the full round trip against a real
// Postgres database. Skipped unless MIGRATOR_TEST_DSN is set.
//
// Run manually:
//
//	MIGRATOR_TEST_DSN="postgres://user:pass@host/db?sslmode=require" \
//	  go test ./tools/migrator -run TestMigratorIntegration -v
func TestMigratorIntegration(t *testing.T) {
	dsn := os.Getenv("MIGRATOR_TEST_DSN")
	if dsn == "" {
		t.Skip("MIGRATOR_TEST_DSN not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	all, err := db.LoadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("expected at least one embedded migration")
	}

	logger := testLogger()
	handle, err := newPGHandle(ctx, dsn, logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer handle.Close()

	// Start from a clean slate: roll everything back if needed.
	applied, err := handle.ReadApplied(ctx)
	if err != nil {
		t.Fatalf("read applied (initial): %v", err)
	}
	if len(applied) > 0 {
		plan := planDown(all, applied, len(applied))
		if err := applyPlan(ctx, handle, plan, dirDown, logger); err != nil {
			t.Fatalf("initial rollback: %v", err)
		}
	}

	// Apply everything in `up` order.
	appliedNow, err := handle.ReadApplied(ctx)
	if err != nil {
		t.Fatalf("read applied (post-reset): %v", err)
	}
	up := planUp(all, appliedNow, 0)
	if len(up) == 0 {
		t.Fatal("expected migrations to apply on a clean database")
	}
	if err := applyPlan(ctx, handle, up, dirUp, logger); err != nil {
		t.Fatalf("apply up: %v", err)
	}

	// Verify the bookkeeping table contains a row per migration with a
	// non-zero applied_at.
	final, err := handle.ReadApplied(ctx)
	if err != nil {
		t.Fatalf("read applied (final): %v", err)
	}
	if len(final) != len(all) {
		t.Fatalf("expected %d applied rows, got %d", len(all), len(final))
	}
	for v, row := range final {
		if row.AppliedAt.IsZero() {
			t.Errorf("version %d has zero applied_at", v)
		}
		if row.Name == "" {
			t.Errorf("version %d has empty name", v)
		}
	}

	// `version` semantics: highest applied must equal the latest known
	// migration.
	wantLatest := all[len(all)-1].Version
	got := 0
	for v := range final {
		if v > got {
			got = v
		}
	}
	if got != wantLatest {
		t.Errorf("highest applied version: want %d, got %d", wantLatest, got)
	}

	// Re-running `up` on a fully-applied database should be a no-op.
	upAgain := planUp(all, final, 0)
	if len(upAgain) != 0 {
		t.Errorf("idempotency violation: %d migrations still pending after full apply", len(upAgain))
	}

	// Roll back exactly one migration and confirm the table contracts.
	down := planDown(all, final, 1)
	if err := applyPlan(ctx, handle, down, dirDown, logger); err != nil {
		t.Fatalf("apply down: %v", err)
	}
	afterDown, err := handle.ReadApplied(ctx)
	if err != nil {
		t.Fatalf("read applied (after down): %v", err)
	}
	if len(afterDown) != len(all)-1 {
		t.Errorf("after down expected %d applied rows, got %d", len(all)-1, len(afterDown))
	}
}
