package main

import (
	"context"
	"errors"
	"leaguewatcher/internal/leaguewatcher/db"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/matryer/is"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// fakeHandle implements dbHandle in memory for unit tests.
type fakeHandle struct {
	applied   map[int]appliedRow
	lockErr   error
	applyErr  map[int]error // version → inject failure on next ApplyMigration
	callOrder []string
}

func newFakeHandle() *fakeHandle {
	return &fakeHandle{
		applied:  map[int]appliedRow{},
		applyErr: map[int]error{},
	}
}

func (f *fakeHandle) TryLock(ctx context.Context) (func(), error) {
	if f.lockErr != nil {
		return nil, f.lockErr
	}
	f.callOrder = append(f.callOrder, "lock")
	return func() { f.callOrder = append(f.callOrder, "unlock") }, nil
}

func (f *fakeHandle) ReadApplied(ctx context.Context) (map[int]appliedRow, error) {
	out := make(map[int]appliedRow, len(f.applied))
	for k, v := range f.applied {
		out[k] = v
	}
	return out, nil
}

func (f *fakeHandle) ApplyMigration(ctx context.Context, m db.Migration, dir direction) error {
	f.callOrder = append(f.callOrder, dirString(dir)+":"+itoa(m.Version))
	if err, ok := f.applyErr[m.Version]; ok {
		return err
	}
	if dir == dirUp {
		f.applied[m.Version] = appliedRow{Version: m.Version, Name: m.Name, AppliedAt: time.Now()}
	} else {
		delete(f.applied, m.Version)
	}
	return nil
}

func (f *fakeHandle) Close() {}

func itoa(i int) string { return string(rune('0' + i)) }

func sampleMigrations() []db.Migration {
	return []db.Migration{
		{Version: 1, Name: "schema_migrations", UpSQL: "-- up1", DownSQL: "-- down1"},
		{Version: 2, Name: "add_users", UpSQL: "-- up2", DownSQL: "-- down2"},
		{Version: 3, Name: "add_matches", UpSQL: "-- up3", DownSQL: "-- down3"},
	}
}

func TestPlanUp_FreshDatabase(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	plan := planUp(sampleMigrations(), map[int]appliedRow{}, 0)
	is.Equal(len(plan), 3)
	is.Equal(plan[0].Version, 1)
	is.Equal(plan[1].Version, 2)
	is.Equal(plan[2].Version, 3)
}

func TestPlanUp_PartiallyApplied(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	applied := map[int]appliedRow{
		1: {Version: 1, Name: "schema_migrations", AppliedAt: time.Now()},
		2: {Version: 2, Name: "add_users", AppliedAt: time.Now()},
	}
	plan := planUp(sampleMigrations(), applied, 0)
	is.Equal(len(plan), 1)
	is.Equal(plan[0].Version, 3)
}

func TestPlanUp_WithTarget(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	plan := planUp(sampleMigrations(), map[int]appliedRow{}, 2)
	is.Equal(len(plan), 2) // only up to version 2
	is.Equal(plan[1].Version, 2)
}

func TestPlanUp_FullyApplied(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	applied := map[int]appliedRow{
		1: {Version: 1, Name: "schema_migrations", AppliedAt: time.Now()},
		2: {Version: 2, Name: "add_users", AppliedAt: time.Now()},
		3: {Version: 3, Name: "add_matches", AppliedAt: time.Now()},
	}
	plan := planUp(sampleMigrations(), applied, 0)
	is.Equal(len(plan), 0)
}

func TestPlanDown_RollsBackMostRecentFirst(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	applied := map[int]appliedRow{
		1: {Version: 1, Name: "schema_migrations", AppliedAt: time.Now()},
		2: {Version: 2, Name: "add_users", AppliedAt: time.Now()},
		3: {Version: 3, Name: "add_matches", AppliedAt: time.Now()},
	}
	plan := planDown(sampleMigrations(), applied, 1)
	is.Equal(len(plan), 1)
	is.Equal(plan[0].Version, 3)
}

func TestPlanDown_MultipleSteps(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	applied := map[int]appliedRow{
		1: {Version: 1, Name: "schema_migrations", AppliedAt: time.Now()},
		2: {Version: 2, Name: "add_users", AppliedAt: time.Now()},
		3: {Version: 3, Name: "add_matches", AppliedAt: time.Now()},
	}
	plan := planDown(sampleMigrations(), applied, 2)
	is.Equal(len(plan), 2)
	is.Equal(plan[0].Version, 3)
	is.Equal(plan[1].Version, 2)
}

func TestPlanDown_NothingApplied(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	plan := planDown(sampleMigrations(), map[int]appliedRow{}, 1)
	is.Equal(len(plan), 0)
}

func TestPlanDown_DefaultsToOne(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	applied := map[int]appliedRow{
		1: {Version: 1, Name: "schema_migrations", AppliedAt: time.Now()},
		2: {Version: 2, Name: "add_users", AppliedAt: time.Now()},
	}
	plan := planDown(sampleMigrations(), applied, 0) // 0 → default 1
	is.Equal(len(plan), 1)
	is.Equal(plan[0].Version, 2)
}

func TestApplyPlan_HappyPathUp(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	h := newFakeHandle()
	err := applyPlan(context.Background(), h, sampleMigrations(), dirUp, testLogger())
	is.NoErr(err)
	is.Equal(len(h.applied), 3)
	// Lock released before the function returns
	is.Equal(h.callOrder[0], "lock")
	is.Equal(h.callOrder[len(h.callOrder)-1], "unlock")
}

func TestApplyPlan_HappyPathDown(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	h := newFakeHandle()
	// Pre-apply 1-3 so we can roll back
	for _, m := range sampleMigrations() {
		h.applied[m.Version] = appliedRow{Version: m.Version, Name: m.Name, AppliedAt: time.Now()}
	}
	h.callOrder = nil // reset

	plan := planDown(sampleMigrations(), h.applied, 1)
	err := applyPlan(context.Background(), h, plan, dirDown, testLogger())
	is.NoErr(err)
	is.Equal(len(h.applied), 2) // 1 and 2 remain
}

func TestApplyPlan_PropagatesMigrationError(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	h := newFakeHandle()
	h.applyErr[2] = errors.New("boom on 2")
	err := applyPlan(context.Background(), h, sampleMigrations(), dirUp, testLogger())

	is.True(err != nil)
	is.Equal(err.Error(), "boom on 2")
	is.Equal(len(h.applied), 1) // only 1 committed; 2 and 3 never ran
}

func TestApplyPlan_PropagatesLockError(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	h := newFakeHandle()
	h.lockErr = ErrLockHeld
	err := applyPlan(context.Background(), h, sampleMigrations(), dirUp, testLogger())

	is.True(errors.Is(err, ErrLockHeld))
	is.Equal(len(h.applied), 0) // no migrations applied when lock fails
}

func TestMigrationStatements_UpOrder(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	m := db.Migration{Version: 1, Name: "schema_migrations", UpSQL: "CREATE TABLE x", DownSQL: "DROP TABLE x"}
	stmts := migrationStatements(m, dirUp)

	is.Equal(len(stmts), 2)
	is.Equal(stmts[0].kind, stmtBody) // DDL first
	is.Equal(stmts[0].sql, "CREATE TABLE x")
	is.Equal(stmts[1].kind, stmtBookkeep) // then INSERT bookkeeping
	is.Equal(stmts[1].args[0], 1)
	is.Equal(stmts[1].args[1], "schema_migrations")
}

// TestMigrationStatements_DownOrder guards the regression where the
// meta-migration's down.sql (which drops schema_migrations) ran before
// the DELETE bookkeeping step, causing 42P01 and a rolled-back txn.
// See commit history for the original `migrate down` failure.
func TestMigrationStatements_DownOrder(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	m := db.Migration{Version: 1, Name: "schema_migrations", UpSQL: "CREATE TABLE x", DownSQL: "DROP TABLE x"}
	stmts := migrationStatements(m, dirDown)

	is.Equal(len(stmts), 2)
	is.Equal(stmts[0].kind, stmtBookkeep) // DELETE first (before the table may be dropped)
	is.Equal(stmts[1].kind, stmtBody)     // then DDL
	is.Equal(stmts[1].sql, "DROP TABLE x")
}

func TestMigrationStatements_EmptyBody(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	m := db.Migration{Version: 2, Name: "noop", UpSQL: "", DownSQL: ""}

	up := migrationStatements(m, dirUp)
	is.Equal(len(up), 1)
	is.Equal(up[0].kind, stmtBookkeep)

	down := migrationStatements(m, dirDown)
	is.Equal(len(down), 1)
	is.Equal(down[0].kind, stmtBookkeep)
}

func TestApplyPlan_EmptyPlanIsNoop(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	h := newFakeHandle()
	err := applyPlan(context.Background(), h, nil, dirUp, testLogger())
	is.NoErr(err)
	is.Equal(len(h.callOrder), 0) // never even tries to lock
}

func TestFormatStatus(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	ts := time.Date(2026, 10, 19, 21, 0, 0, 0, time.UTC)
	rep := statusReport{
		Applied: []appliedRow{
			{Version: 1, Name: "schema_migrations", AppliedAt: ts},
		},
		Pending: []db.Migration{
			{Version: 2, Name: "add_users"},
		},
	}
	out := formatStatus(rep)
	is.True(strings.Contains(out, "applied: 1"))
	is.True(strings.Contains(out, "pending: 1"))
	is.True(strings.Contains(out, "schema_migrations"))
	is.True(strings.Contains(out, "add_users"))
	is.True(strings.Contains(out, "2026-10-19T21:00:00Z"))
}

func TestReadStatus_SortsDeterministically(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	h := newFakeHandle()
	// Insert deliberately out of order
	h.applied[3] = appliedRow{Version: 3, Name: "add_matches", AppliedAt: time.Now()}
	h.applied[1] = appliedRow{Version: 1, Name: "schema_migrations", AppliedAt: time.Now()}

	rep, err := readStatus(context.Background(), h, sampleMigrations())
	is.NoErr(err)
	is.Equal(len(rep.Applied), 2)
	is.Equal(rep.Applied[0].Version, 1)
	is.Equal(rep.Applied[1].Version, 3)
	is.Equal(len(rep.Pending), 1)
	is.Equal(rep.Pending[0].Version, 2)
}
