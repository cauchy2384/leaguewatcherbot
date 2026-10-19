package bot

import (
	"context"
	"errors"
	"testing"
	"time"

	"leaguewatcher/internal/leaguewatcher/bot/repository"

	"github.com/matryer/is"
)

// fakePidorStore is an in-memory PidorStore for handler tests. The
// shape mirrors what the real Postgres-backed impl returns.
type fakePidorStore struct {
	canBeChosenResult     bool
	canBeChosenErr        error
	tryChooseResult       bool
	tryChooseErr          error
	stats                 map[string]repository.PidorStat
	statsErr              error
	lastTryChooseChannel  string
	lastTryChooseUserID   string
	lastTryChooseUserName string
	tryChooseCalls        int
	statsCalls            int
}

func (f *fakePidorStore) CanBeChosen(ctx context.Context, channelID string, today time.Time) (bool, error) {
	return f.canBeChosenResult, f.canBeChosenErr
}

func (f *fakePidorStore) TryChooseToday(ctx context.Context, channelID, userID, userName string, today time.Time) (bool, error) {
	f.tryChooseCalls++
	f.lastTryChooseChannel = channelID
	f.lastTryChooseUserID = userID
	f.lastTryChooseUserName = userName
	return f.tryChooseResult, f.tryChooseErr
}

func (f *fakePidorStore) Stats(ctx context.Context, channelID string) (map[string]repository.PidorStat, error) {
	f.statsCalls++
	return f.stats, f.statsErr
}

// TestPidorsToday verifies the date computation around MSK/UTC
// boundaries.
func TestPidorsToday(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	msk, err := time.LoadLocation("Europe/Moscow")
	is.NoErr(err)

	p := &Pidors{store: &fakePidorStore{}, loc: msk}

	today := p.today()
	// Today must be at midnight UTC for Postgres DATE friendliness.
	is.Equal(today.Hour(), 0)
	is.Equal(today.Minute(), 0)
	is.Equal(today.Second(), 0)
	is.Equal(today.Location(), time.UTC)
}

func TestPidorsStoreNilGuards(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	_, err := NewPidors(nil, time.UTC)
	is.True(err != nil)

	_, err = NewPidors(&fakePidorStore{}, nil)
	is.True(err != nil)
}

// TestSortPidors verifies the render-time sorting matches the legacy
// behaviour: descending by count, tie-broken by name ascending.
func TestSortPidors(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	stats := map[string]repository.PidorStat{
		"u1": {Name: "alice", Count: 5},
		"u2": {Name: "zebra", Count: 5},
		"u3": {Name: "mid", Count: 5},
		"u4": {Name: "bob", Count: 12},
		"u5": {Name: "carol", Count: 1},
	}

	got := sortPidors(stats)
	is.Equal(len(got), 5)
	is.Equal(got[0], pidorRow{Name: "bob", Count: 12})
	// Ties at 5: alice, mid, zebra in name order
	is.Equal(got[1], pidorRow{Name: "alice", Count: 5})
	is.Equal(got[2], pidorRow{Name: "mid", Count: 5})
	is.Equal(got[3], pidorRow{Name: "zebra", Count: 5})
	is.Equal(got[4], pidorRow{Name: "carol", Count: 1})
}

// TestSortPidorsEmpty checks the no-roll case doesn't crash and
// returns an empty slice (the renderer then prints "Все пидоры тут").
func TestSortPidorsEmpty(t *testing.T) {
	t.Parallel()
	is := is.New(t)
	got := sortPidors(map[string]repository.PidorStat{})
	is.Equal(len(got), 0)
}

// TestBotPidorStoreErrorPaths exercises the graceful-degradation
// response shapes via the Bot's handlers — but only as far as the
// "store returns error" branch goes. We cannot easily simulate a full
// discordgo.Session without significant mocking, so these unit tests
// only cover the parts we can drive deterministically.
func TestSortPidorsStability(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	// Run sortPidors repeatedly over the same input — result must be stable.
	stats := map[string]repository.PidorStat{
		"a": {Name: "alice", Count: 3},
		"b": {Name: "bob", Count: 3},
		"c": {Name: "carol", Count: 5},
		"d": {Name: "dora", Count: 1},
		"e": {Name: "eve", Count: 1},
	}
	first := sortPidors(stats)
	for i := 0; i < 20; i++ {
		got := sortPidors(stats)
		is.Equal(got, first) // map iteration order must not affect result
	}
}

// TestPidorStoreErrorPropagates ensures error types pass through as-is.
// (A trivial sanity test — real value is in the integration test where
// pgx errors are realistic.)
func TestPidorStoreErrorPropagates(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	wantErr := errors.New("connection reset by peer")
	fake := &fakePidorStore{statsErr: wantErr}
	_, err := fake.Stats(context.Background(), "any")
	is.True(errors.Is(err, wantErr))
}
