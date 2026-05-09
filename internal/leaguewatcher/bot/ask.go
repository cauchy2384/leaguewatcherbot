package bot

import (
	"context"
	"fmt"
	"leaguewatcher/internal/leaguewatcher"
	"time"

	"cloud.google.com/go/vertexai/genai"
	"github.com/bwmarrin/discordgo"
	"google.golang.org/api/option"
)

// generateWithVertexAI calls Vertex AI with the given configuration and query
func generateWithVertexAI(ctx context.Context, cfg leaguewatcher.Config, query string) (string, error) {
	// Validate required fields
	if cfg.GCPSA == "" || cfg.GCPProjectID == "" {
		return "", fmt.Errorf("vertex ai not configured")
	}

	// Create Vertex AI client
	client, err := genai.NewClient(ctx, cfg.GCPProjectID, cfg.GCPLocation,
		option.WithCredentialsJSON([]byte(cfg.GCPSA)))
	if err != nil {
		return "", fmt.Errorf("failed to create vertex ai client: %w", err)
	}
	defer client.Close()

	// Get generative model
	model := client.GenerativeModel(cfg.GeminiModel)

	// Set system instruction if provided
	if cfg.GeminiSystemPrompt != "" {
		model.SystemInstruction = &genai.Content{
			Parts: []genai.Part{genai.Text(cfg.GeminiSystemPrompt)},
		}
	}

	// Generate content
	resp, err := model.GenerateContent(ctx, genai.Text(query))
	if err != nil {
		return "", fmt.Errorf("failed to generate content: %w", err)
	}

	// Extract text from response
	if len(resp.Candidates) == 0 {
		return "", nil // Empty response
	}

	var text string
	for _, part := range resp.Candidates[0].Content.Parts {
		if t, ok := part.(genai.Text); ok {
			text += string(t)
		}
	}

	return text, nil
}

func (b *Bot) ask(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, query string) {
	// Get config (hot-reload via ConfigManager)
	cfg := b.configMgr.Get()

	// Set timeout
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Call Vertex AI
	answer, err := generateWithVertexAI(ctx, cfg, query)
	if err != nil {
		if err.Error() == "vertex ai not configured" {
			s.ChannelMessageSendReply(m.ChannelID, "Я не настроен для этого, извини!", m.Reference())
			b.logger.Error("vertex ai not configured")
		} else {
			s.ChannelMessageSendReply(m.ChannelID, "У меня проблемы со связью с оракулом.", m.Reference())
			b.logger.Error("failed to call vertex ai",
				"error", err,
				"model", cfg.GeminiModel,
				"project", cfg.GCPProjectID,
				"location", cfg.GCPLocation)
		}
		return
	}

	if answer == "" {
		s.ChannelMessageSendReply(m.ChannelID, "Оракул хранит молчание.", m.Reference())
		return
	}

	s.ChannelMessageSendReply(m.ChannelID, answer, m.Reference())
}
