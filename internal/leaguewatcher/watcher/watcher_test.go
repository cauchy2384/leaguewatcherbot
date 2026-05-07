package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"leaguewatcher/internal/leaguewatcher"

	"github.com/matryer/is"
)

func TestWatcher(t *testing.T) {
	t.Skip("\todo fix this test")

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

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
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
