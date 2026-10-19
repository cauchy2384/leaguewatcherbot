package bot

import (
	"context"
	"leaguewatcher/internal/leaguewatcher"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestTavilyIntegration(t *testing.T) {
	token := os.Getenv("DOPPLER_TOKEN")
	if token == "" {
		t.Skip("DOPPLER_TOKEN not set, skipping integration test")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cm, err := leaguewatcher.NewConfigManager(token, logger)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := cm.Reload(ctx); err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	cfg := cm.Get()
	if cfg.TavilyAPIKey == "" {
		t.Skip("Tavily not configured in Doppler (TAVILY_API_KEY missing), skipping integration test")
	}

	results, err := searchTavily(ctx, cfg, "WoW Ascension Bronzebeard Onyxia strategy")
	if err != nil {
		t.Fatalf("failed to call tavily: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("tavily returned 0 results for a known-good query")
	}

	for i, r := range results {
		if r.Title == "" || r.URL == "" || r.Content == "" {
			t.Fatalf("result %d missing fields: %+v", i, r)
		}
	}

	t.Logf("Tavily returned %d results, top hit: %q (%s)", len(results), results[0].Title, results[0].URL)
}
