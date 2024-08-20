package leaguewatcher

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Match struct {
	ID        int
	StartedAt time.Time
	Duration  int

	Player   Player
	Champion Champion
	Queue    Queue
	Role     string

	Win bool

	Kills   int
	Deaths  int
	Assists int

	LP *int
}

type Queue string

const (
	QueueNormalDraft Queue = "Normal draft"
	QueueRankedSolo  Queue = "Ranked solo"
	QueueRankedFlex  Queue = "Ranked flex"
	QueueARAM        Queue = "ARAM"
)

func (m Match) FinishedAt() time.Time {
	return m.StartedAt.Add(time.Duration(m.Duration) * time.Second)
}

func (m Match) URL() string {
	return fmt.Sprintf(
		"https://app.mobalytics.gg/lol/match/%s/%s-%s/%d",
		strings.ToLower(m.Player.Region),
		url.PathEscape(strings.ToLower(m.Player.Name)),
		url.PathEscape(strings.ToLower(m.Player.Tag)),
		m.ID,
	)
}
