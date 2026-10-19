package main

import (
	"leaguewatcher/internal/leaguewatcher/pidorjson"
	"strings"
	"testing"
	"time"

	"github.com/matryer/is"
)

// mustDate parses an ISO date string into a time.Time at UTC midnight.
func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestComputePlan(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		input             *pidorjson.Pidor
		wantRows          []planRow
		wantWarningSubstr []string
	}{
		{
			name:     "nil pidor returns empty plan",
			input:    nil,
			wantRows: nil,
		},
		{
			name:     "empty stats returns empty plan",
			input:    &pidorjson.Pidor{Stats: map[string]map[string]pidorjson.PidorStat{}},
			wantRows: nil,
		},
		{
			name: "one channel, one user, epoch date",
			input: &pidorjson.Pidor{Stats: map[string]map[string]pidorjson.PidorStat{
				"chan1": {"u1": {Name: "alice", Count: 5}},
			}},
			wantRows: []planRow{
				{ChannelID: "chan1", UserID: "u1", UserName: "alice",
					RolledOn: mustDate("1970-01-01"), TimesRolled: 5},
			},
		},
		{
			name: "one channel, three users get sequential dates in lexicographic order",
			input: &pidorjson.Pidor{Stats: map[string]map[string]pidorjson.PidorStat{
				"chan1": {
					"u1": {Name: "alice", Count: 3},
					"u9": {Name: "carol", Count: 7},
					"u5": {Name: "bob", Count: 2},
				},
			}},
			wantRows: []planRow{
				{ChannelID: "chan1", UserID: "u1", UserName: "alice", RolledOn: mustDate("1970-01-01"), TimesRolled: 3},
				{ChannelID: "chan1", UserID: "u5", UserName: "bob", RolledOn: mustDate("1970-01-02"), TimesRolled: 2},
				{ChannelID: "chan1", UserID: "u9", UserName: "carol", RolledOn: mustDate("1970-01-03"), TimesRolled: 7},
			},
		},
		{
			name: "two channels each get their own date sequence from epoch",
			input: &pidorjson.Pidor{Stats: map[string]map[string]pidorjson.PidorStat{
				"chanA": {
					"u1": {Name: "alice", Count: 1},
					"u2": {Name: "bob", Count: 2},
				},
				"chanB": {
					"u1": {Name: "alice", Count: 4},
				},
			}},
			wantRows: []planRow{
				{ChannelID: "chanA", UserID: "u1", UserName: "alice", RolledOn: mustDate("1970-01-01"), TimesRolled: 1},
				{ChannelID: "chanA", UserID: "u2", UserName: "bob", RolledOn: mustDate("1970-01-02"), TimesRolled: 2},
				{ChannelID: "chanB", UserID: "u1", UserName: "alice", RolledOn: mustDate("1970-01-01"), TimesRolled: 4},
			},
		},
		{
			name: "zero-count user is skipped with warning",
			input: &pidorjson.Pidor{Stats: map[string]map[string]pidorjson.PidorStat{
				"chan1": {
					"u1": {Name: "alice", Count: 3},
					"u2": {Name: "ghost", Count: 0},
					"u3": {Name: "bob", Count: 2},
				},
			}},
			wantRows: []planRow{
				{ChannelID: "chan1", UserID: "u1", UserName: "alice", RolledOn: mustDate("1970-01-01"), TimesRolled: 3},
				{ChannelID: "chan1", UserID: "u3", UserName: "bob", RolledOn: mustDate("1970-01-02"), TimesRolled: 2},
			},
			wantWarningSubstr: []string{"count=0"},
		},
		{
			name: "negative-count user is skipped with warning",
			input: &pidorjson.Pidor{Stats: map[string]map[string]pidorjson.PidorStat{
				"chan1": {
					"u1": {Name: "alice", Count: 1},
					"u2": {Name: "neg", Count: -5},
				},
			}},
			wantRows: []planRow{
				{ChannelID: "chan1", UserID: "u1", UserName: "alice", RolledOn: mustDate("1970-01-01"), TimesRolled: 1},
			},
			wantWarningSubstr: []string{"count=-5"},
		},
		{
			name: "empty-name user is skipped with warning",
			input: &pidorjson.Pidor{Stats: map[string]map[string]pidorjson.PidorStat{
				"chan1": {
					"u1": {Name: "alice", Count: 3},
					"u2": {Name: "", Count: 7},
				},
			}},
			wantRows: []planRow{
				{ChannelID: "chan1", UserID: "u1", UserName: "alice", RolledOn: mustDate("1970-01-01"), TimesRolled: 3},
			},
			wantWarningSubstr: []string{"empty name"},
		},
		{
			name: "non-numeric user ids still sort lexicographically",
			input: &pidorjson.Pidor{Stats: map[string]map[string]pidorjson.PidorStat{
				"chan1": {
					"zebra": {Name: "z", Count: 1},
					"alpha": {Name: "a", Count: 2},
					"mango": {Name: "m", Count: 3},
				},
			}},
			wantRows: []planRow{
				{ChannelID: "chan1", UserID: "alpha", UserName: "a", RolledOn: mustDate("1970-01-01"), TimesRolled: 2},
				{ChannelID: "chan1", UserID: "mango", UserName: "m", RolledOn: mustDate("1970-01-02"), TimesRolled: 3},
				{ChannelID: "chan1", UserID: "zebra", UserName: "z", RolledOn: mustDate("1970-01-03"), TimesRolled: 1},
			},
		},
		{
			name: "deterministic across runs (maps do not shuffle the plan)",
			input: &pidorjson.Pidor{Stats: map[string]map[string]pidorjson.PidorStat{
				"chanX": {
					"u9": {Name: "nine", Count: 90},
					"u1": {Name: "one", Count: 10},
					"u5": {Name: "five", Count: 50},
				},
			}},
			wantRows: []planRow{
				{ChannelID: "chanX", UserID: "u1", UserName: "one", RolledOn: mustDate("1970-01-01"), TimesRolled: 10},
				{ChannelID: "chanX", UserID: "u5", UserName: "five", RolledOn: mustDate("1970-01-02"), TimesRolled: 50},
				{ChannelID: "chanX", UserID: "u9", UserName: "nine", RolledOn: mustDate("1970-01-03"), TimesRolled: 90},
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := is.New(t)

			gotRows, gotWarnings := computePlan(tt.input)

			is.Equal(len(gotRows), len(tt.wantRows))
			for i, want := range tt.wantRows {
				got := gotRows[i]
				is.Equal(got.ChannelID, want.ChannelID)
				is.Equal(got.UserID, want.UserID)
				is.Equal(got.UserName, want.UserName)
				is.True(got.RolledOn.Equal(want.RolledOn))
				is.Equal(got.TimesRolled, want.TimesRolled)
			}

			is.Equal(len(gotWarnings), len(tt.wantWarningSubstr))
			for i, substr := range tt.wantWarningSubstr {
				is.True(strings.Contains(gotWarnings[i], substr))
			}
		})
	}
}

// TestSyntheticEpochInPast guards that our hard-coded epoch is
// unambiguously in the past. If anyone ever bumps this to a date the
// bot could actually see, the build-correctness invariant fails.
func TestSyntheticEpochInPast(t *testing.T) {
	t.Parallel()
	if !syntheticEpoch.Before(time.Now()) {
		t.Fatalf("syntheticEpoch %s must be in the past", syntheticEpoch)
	}
}
