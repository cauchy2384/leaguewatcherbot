package db_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"leaguewatcher/internal/leaguewatcher"
	"leaguewatcher/internal/leaguewatcher/db"

	"github.com/matryer/is"
)

// TestIntegration_DopplerToNeon exercises the full startup wiring end-to-end:
// fetch secrets from Doppler, build the config, take DATABASE_URL_POOLED
// (falling back to DATABASE_URL), then connect to Neon and ping it.
//
// Skipped by default; requires DOPPLER_TOKEN and a DATABASE_URL(_POOLED)
// secret present in the Doppler project/config.
func TestIntegration_DopplerToNeon(t *testing.T) {
	t.Parallel()

	dopplerToken := os.Getenv("DOPPLER_TOKEN")
	if dopplerToken == "" {
		t.Skip("DOPPLER_TOKEN is not set")
	}

	is := is.New(t)
	ctx := context.Background()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during tests
	}))

	// 1. Load config from real Doppler
	cm, err := leaguewatcher.NewConfigManager(dopplerToken, logger)
	is.NoErr(err)

	err = cm.Reload(ctx)
	is.NoErr(err)

	cfg := cm.Get()

	// 2. Same selection logic as cmd/leaguewatcher/main.go
	databaseURL := cfg.DatabaseURLPooled
	if databaseURL == "" {
		databaseURL = cfg.DatabaseURL
	}
	if databaseURL == "" {
		t.Skip("no DATABASE_URL(_POOLED) configured in Doppler")
	}

	// 3. Connect to Neon and verify with a ping
	pool, err := db.Connect(ctx, databaseURL, logger)
	is.NoErr(err)
	defer pool.Close()

	is.NoErr(pool.Ping(ctx))
}
