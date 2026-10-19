package bot

import (
	"context"
	"leaguewatcher/internal/leaguewatcher"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestTokenFactoryIntegration(t *testing.T) {
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
	if cfg.TofaAPIKey == "" || cfg.TofaModel == "" {
		t.Skip("Token Factory not configured in Doppler (TOFA_API_KEY or TOFA_MODEL missing), skipping integration test")
	}

	// Test with Russian query using shared helper
	query := "Сколько боссов в бвл?"
	messages := buildMessages(query, cfg.TofaSystemPrompt)
	answer, err := generateWithTokenFactory(ctx, cfg, messages)
	if err != nil {
		t.Fatalf("failed to call token factory: %v", err)
	}

	if answer == "" {
		t.Fatal("token factory returned an empty answer")
	}

	t.Logf("Token Factory response: %s", answer)
}
