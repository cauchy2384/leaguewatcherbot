package leaguewatcher

import (
	"testing"

	"github.com/matryer/is"
)

func TestMatchURL(t *testing.T) {
	is := is.New(t)

	m := Match{
		Player: Player{
			Region: "euw",
			Name:   "satum quata",
			Tag:    "EUW",
		},
		ID: 12345,
	}

	url := m.URL()
	const expected = "https://app.mobalytics.gg/lol/match/euw/satum%20quata-euw/12345"

	is.Equal(url, expected)
}
