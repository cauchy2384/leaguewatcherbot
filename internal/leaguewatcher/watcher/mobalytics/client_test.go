package mobalytics

import (
	"context"
	"fmt"
	"testing"

	"github.com/matryer/is"
	"go.uber.org/zap"
)

func TestClientMatches(t *testing.T) {
	ctx := context.Background()
	client := NewClient(zap.NewNop())

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
	ctx := context.Background()
	client := NewClient(zap.NewNop())

	is := is.New(t)

	champs, err := client.Champions(ctx)
	fmt.Println(champs)
	is.NoErr(err)
	is.True(len(champs) > 0)

	fmt.Println(len(champs))
}

func TestRefreshProfiles(t *testing.T) {
	ctx := context.Background()

	client := NewClient(zap.NewNop())

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

	for i := range testCases {
		tt := testCases[i]

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			is := is.New(t)

			res, err := client.RefreshProfile(ctx, tt.region, tt.name, tt.tag)
			is.NoErr(err)
			fmt.Println(tt.name, res)
		})
	}
}
