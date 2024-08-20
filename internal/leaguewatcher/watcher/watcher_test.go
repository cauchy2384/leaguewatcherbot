package watcher

import (
	"context"
	"fmt"
	"leaguewatcher/internal/leaguewatcher"
	"testing"
	"time"

	"github.com/matryer/is"
	"go.uber.org/zap"
)

func TestWatcher(t *testing.T) {
	ctx := context.Background()
	is := is.New(t)

	cfg := Config{
		Period:    5 * time.Second,
		PlayedGap: 7 * 24 * time.Hour,

		Players: []leaguewatcher.Player{
			{Region: "euw", Name: "omensielvo"},
			{Region: "euw", Name: "willy2barrels"},
		},
	}

	logger, _ := zap.NewDevelopment()
	watcher := New(cfg, logger)

	ctx, cancel := context.WithTimeout(ctx, 3*cfg.Period)

	ch, done := watcher.Run(ctx)

	var matches []leaguewatcher.Match
	for match := range ch {
		matches = append(matches, match)
	}

	cancel()
	<-done

	fmt.Println(matches)
	is.Equal(len(matches), len(cfg.Players))

	for _, m := range matches {
		s := fmt.Sprintf("[%s] %s %s - %d/%d/%d", m.Queue, m.Champion.Name, m.Role, m.Kills, m.Deaths, m.Assists)
		fmt.Println(s)
	}
}
