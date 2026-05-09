package bot

import (
	"context"
	"leaguewatcher/internal/leaguewatcher"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestVertexAIIntegration(t *testing.T) {
	token := os.Getenv("DOPPLER_TOKEN")
	if token == "" {
		t.Skip("DOPPLER_TOKEN not set, skipping integration test")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cm, err := leaguewatcher.NewConfigManager(token, logger)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := cm.Reload(ctx); err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	cfg := cm.Get()
	if cfg.GCPSA == "" || cfg.GCPProjectID == "" {
		t.Skip("Vertex AI not configured in Doppler (GCP_SA or GCP_PROJECT_ID missing), skipping integration test")
	}

	// Test with Russian query using shared helper
	query := "Сколько боссов в бвл?"
	answer, err := generateWithVertexAI(ctx, cfg, query)
	if err != nil {
		t.Fatalf("failed to call vertex ai: %v", err)
	}

	if answer == "" {
		t.Fatal("vertex ai returned an empty answer")
	}

	t.Logf("Vertex AI response: %s", answer)
}
