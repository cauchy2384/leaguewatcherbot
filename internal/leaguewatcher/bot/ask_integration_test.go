package bot

import (
	"context"
	"leaguewatcher/internal/leaguewatcher"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// TestAskPipeline_LatestLoLPatch exercises the full grounded Q&A path end
// to end against production providers:
//
//	searchTavily → buildMessages → generateWithTokenFactory
//
// The query ("what is the latest LoL patch") deliberately targets facts
// that change faster than any model's training cutoff — only the search
// step can bring the answer up to date. Skipped unless both TOFA_* and
// TAVILY_* are configured in Doppler.
func TestAskPipeline_LatestLoLPatch(t *testing.T) {
	token := os.Getenv("DOPPLER_TOKEN")
	if token == "" {
		t.Skip("DOPPLER_TOKEN not set, skipping integration test")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
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
	if cfg.TavilyAPIKey == "" {
		t.Skip("Tavily not configured in Doppler (TAVILY_API_KEY missing), skipping integration test")
	}

	// 1. Search step
	searchCtx, searchCancel := context.WithTimeout(ctx, time.Duration(cfg.TavilyTimeoutSeconds)*time.Second)
	results, err := searchTavily(searchCtx, cfg, "What is the latest patch in League of Legends?")
	searchCancel()
	if err != nil {
		t.Fatalf("tavily search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("tavily returned 0 results; cannot verify grounding")
	}
	t.Logf("Tavily returned %d results, top hit: %q (%s)", len(results), results[0].Title, results[0].URL)

	// 2. Message composition (system prompt + Tavily context + user query)
	messages := buildMessages("What is the latest patch in League of Legends?", cfg.TofaSystemPrompt, results)

	// 3. LLM call
	answer, err := generateWithTokenFactory(ctx, cfg, messages)
	if err != nil {
		t.Fatalf("failed to call token factory: %v", err)
	}
	if answer == "" {
		t.Fatal("ask pipeline returned an empty answer")
	}

	t.Logf("Ask pipeline answer: %s", answer)

	// Loose sanity checks: a useful answer should reference a patch number
	// (digits with a dot: "14.20", "25.07", ...) or the word "patch"/"патч".
	lowered := strings.ToLower(answer)
	hasPatchWord := strings.Contains(lowered, "patch") || strings.Contains(lowered, "патч")
	hasDigits := strings.ContainsAny(answer, "0123456789")
	if !hasPatchWord || !hasDigits {
		t.Errorf("answer does not look like a patch reference (patch=%v, digits=%v): %s", hasPatchWord, hasDigits, answer)
	}
}
