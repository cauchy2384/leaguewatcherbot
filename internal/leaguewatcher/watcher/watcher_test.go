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

// mockConfigProvider is a mock implementation of ConfigProvider for testing
type mockConfigProvider struct {
	players []leaguewatcher.Player
	config  leaguewatcher.Config
}

func (m *mockConfigProvider) GetPlayers() []leaguewatcher.Player {
	return m.players
}

func (m *mockConfigProvider) Get() leaguewatcher.Config {
	return m.config
}

func TestWatcher(t *testing.T) {
	t.Skip("\todo fix this test")

	is := is.New(t)

	players := []leaguewatcher.Player{
		{Region: "euw", Name: "omensielvo"},
		{Region: "euw", Name: "willy2barrels"},
	}

	cfg := Config{
		Period:    5 * time.Second,
		PlayedGap: 7 * 24 * time.Hour,
	}

	configProvider := &mockConfigProvider{
		players: players,
		config: leaguewatcher.Config{
			PollPeriod: cfg.Period,
			PlayedGap:  cfg.PlayedGap,
			Players:    players,
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	watcher := New(cfg, configProvider, logger)

	ctx, cancel := context.WithTimeout(t.Context(), 3*cfg.Period)

	ch, done := watcher.Run(ctx)

	var matches []leaguewatcher.Match
	for match := range ch {
		matches = append(matches, match)
	}

	cancel()
	<-done

	fmt.Println(matches)
	is.Equal(len(matches), len(players))

	for _, m := range matches {
		s := fmt.Sprintf("[%s] %s %s - %d/%d/%d", m.Queue, m.Champion.Name, m.Role, m.Kills, m.Deaths, m.Assists)
		fmt.Println(s)
	}
}
