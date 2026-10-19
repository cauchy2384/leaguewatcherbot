package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"leaguewatcher/internal/leaguewatcher"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// ErrLLMNotConfigured is returned when the LLM provider is missing required
// configuration (API key, model, etc.). The caller maps it to a user-facing
// message instead of treating it as a transient failure.
var ErrLLMNotConfigured = errors.New("llm not configured")

// tofaHTTPClient is shared across calls. *http.Client is safe for concurrent
// use; the per-call timeout is enforced via the request context in Bot.ask.
var tofaHTTPClient = &http.Client{}

// tofaMessage is a single chat message in the OpenAI-compatible schema.
type tofaMessage struct {
	Role    string `json:"role"` // "system" | "user" | "assistant"
	Content string `json:"content"`
}

// tofaChatRequest is the OpenAI-compatible chat.completions request body.
type tofaChatRequest struct {
	Model       string        `json:"model"`
	Messages    []tofaMessage `json:"messages"`
	Temperature float32       `json:"temperature"`
	TopP        float32       `json:"top_p"`
	MaxTokens   int32         `json:"max_tokens"`
}

// tofaChatResponse is the subset of the OpenAI chat.completions response we
// consume. The error branch is populated on non-2xx responses.
type tofaChatResponse struct {
	Choices []struct {
		Message tofaMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// generateWithTokenFactory calls Nebius Token Factory with the given query
// and returns the assistant reply. The API is OpenAI-compatible; we use the
// stdlib net/http client to avoid pulling a third-party SDK.
func generateWithTokenFactory(ctx context.Context, cfg leaguewatcher.Config, query string) (string, error) {
	if cfg.TofaAPIKey == "" || cfg.TofaModel == "" {
		return "", ErrLLMNotConfigured
	}

	messages := make([]tofaMessage, 0, 2)
	if cfg.TofaSystemPrompt != "" {
		messages = append(messages, tofaMessage{Role: "system", Content: cfg.TofaSystemPrompt})
	}
	messages = append(messages, tofaMessage{Role: "user", Content: query})

	reqBody := tofaChatRequest{
		Model:       cfg.TofaModel,
		Messages:    messages,
		Temperature: cfg.TofaTemperature,
		TopP:        cfg.TofaTopP,
		MaxTokens:   cfg.TofaMaxOutputTokens,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := strings.TrimRight(cfg.TofaBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.TofaAPIKey)

	resp, err := tofaHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call token factory: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token factory returned %s: %s", resp.Status, truncateForLog(body))
	}

	var parsed tofaChatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if parsed.Error != nil {
		return "", fmt.Errorf("token factory error (%s): %s", parsed.Error.Type, parsed.Error.Message)
	}

	if len(parsed.Choices) == 0 {
		return "", nil // Empty response: caller sends "Оракул хранит молчание."
	}

	return parsed.Choices[0].Message.Content, nil
}

// truncateForLog caps long error bodies so they don't blow up the log line.
func truncateForLog(b []byte) string {
	const max = 512
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}

func (b *Bot) ask(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, query string) {
	// Get config (hot-reload via ConfigManager)
	cfg := b.configMgr.Get()

	// Set timeout
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Call Nebius Token Factory
	answer, err := generateWithTokenFactory(ctx, cfg, query)
	if err != nil {
		if errors.Is(err, ErrLLMNotConfigured) {
			s.ChannelMessageSendReply(m.ChannelID, "Я не настроен для этого, извини!", m.Reference())
			b.logger.Error("llm not configured")
		} else {
			s.ChannelMessageSendReply(m.ChannelID, "У меня проблемы со связью с оракулом.", m.Reference())
			b.logger.Error("failed to call token factory",
				"error", err,
				"model", cfg.TofaModel,
				"base_url", cfg.TofaBaseURL)
		}
		return
	}

	if answer == "" {
		s.ChannelMessageSendReply(m.ChannelID, "Оракул хранит молчание.", m.Reference())
		return
	}

	s.ChannelMessageSendReply(m.ChannelID, answer, m.Reference())
}
