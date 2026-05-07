package mobalytics

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/matryer/is"
)

func TestClientMatches(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	client := NewClient(slog.New(slog.NewTextHandler(io.Discard, nil)))

	testCases := []struct {
		name   string
		tag    string
		region string
	}{
		{"omensielvo", "euw", "euw"},
		{"willy2barrels", "euw", "euw"},
		{"serj", "wtf", "euw"},
		{"commanderserj", "euw", "euw"},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			matches, err := client.Matches(ctx, tt.region, tt.name, tt.tag)
			fmt.Println(matches)
			is.NoErr(err)
			is.True(len(matches) > 0)
		})
	}
}

func TestClientChampions(t *testing.T) {
	ctx := t.Context()
	client := NewClient(slog.New(slog.NewTextHandler(io.Discard, nil)))

	is := is.New(t)

	champs, err := client.Champions(ctx)
	fmt.Println(champs)
	is.NoErr(err)
	is.True(len(champs) > 0)

	fmt.Println(len(champs))
}

func TestRefreshProfiles(t *testing.T) {
	t.Skip("\todo fix this test")

	ctx := t.Context()

	client := NewClient(slog.New(slog.NewTextHandler(io.Discard, nil)))

	testCases := []struct {
		name   string
		tag    string
		region string
	}{
		{"koshee", "euw", "euw"},
		{"omensielvo", "euw", "euw"},
		{"willy2barrels", "euw", "euw"},
		{"spielywilly", "euw", "euw"},
		{"kokallika", "euw", "euw"},
		{"satum quata", "euw", "euw"},
		{"x9 critical dmg", "euw", "euw"},
		{"lavrik", "euw", "euw"},
		{"serj", "wtf", "euw"},
		{"commanderserj", "euw", "euw"},
		{"commandershepard", "euw", "euw"},
		{"baumanpower", "euw", "euw"},
		{"haribulus", "harib", "euw"},
		{"hannibalcannibal", "euw", "euw"},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			is := is.New(t)

			res, err := client.RefreshProfile(ctx, tt.region, tt.name, tt.tag)
			is.NoErr(err)
			fmt.Println(tt.name, res)
		})
	}
}
