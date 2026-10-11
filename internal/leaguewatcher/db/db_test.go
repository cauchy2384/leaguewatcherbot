package db

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/matryer/is"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during tests
	}))
}

func TestConnect_Errors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		url        string
		wantErrSub string
	}{
		{
			name:       "empty URL",
			url:        "",
			wantErrSub: "database URL is empty",
		},
		{
			name:       "invalid URL",
			url:        "not-a-valid-url",
			wantErrSub: "failed to parse database URL",
		},
		{
			// Port 1 is reserved and refuses connections immediately.
			name:       "unreachable host",
			url:        "postgres://user:pass@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1",
			wantErrSub: "",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := is.New(t)

			pool, err := Connect(context.Background(), tt.url, testLogger())
			is.True(err != nil) // should error
			if tt.wantErrSub != "" {
				is.True(strings.Contains(err.Error(), tt.wantErrSub)) // error should mention the cause
			}
			is.True(pool == nil) // pool must be nil on failure
		})
	}
}

// TestConnect_Integration verifies connectivity against a real Neon database.
// Skipped by default; set DATABASE_URL to enable.
func TestConnect_Integration(t *testing.T) {
	t.Parallel()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	is := is.New(t)

	pool, err := Connect(context.Background(), databaseURL, testLogger())
	is.NoErr(err)
	defer pool.Close()

	is.NoErr(pool.Ping(context.Background()))
}
