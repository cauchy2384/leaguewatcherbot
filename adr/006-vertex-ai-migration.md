# ADR 006: Vertex AI Migration for Gemini Integration

## Status

**Proposed** - 2026-05-09

## Context

The League Watcher Bot currently uses Google AI Studio's Gemini API for natural language interaction (implemented in ADR 005). However, **Google AI Studio has regional restrictions that block access from certain locations**, making the bot's AI features completely unavailable.

### Problems with Current Approach

- **Regional blocking**: AI Studio API returns errors from restricted regions
- **No workaround available**: VPN/proxy solutions are unreliable for production
- **Core feature unavailable**: Bot's natural language interaction is broken
- **User experience degraded**: Russian Discord users cannot interact with the bot via mentions

### Requirements for Solution

1. **Global availability**: Must work from any region without restrictions
2. **Same model support**: Continue using Gemini 2.0 Flash for performance
3. **Backward compatibility**: Maintain existing bot architecture and Discord UX
4. **Secure authentication**: Use service accounts instead of API keys
5. **Hot-reload support**: Config changes must apply without restart (existing pattern)
6. **Russian language**: All error messages in Russian for target audience

## Decision

We will **migrate from Google AI Studio to Vertex AI** using the official Go SDK with the following approach:

### 1. Replace HTTP Client with Vertex AI SDK

**Current implementation** (`internal/leaguewatcher/bot/ask.go`):
- Manual HTTP client calling `https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent`
- Hand-crafted JSON request/response parsing
- API key authentication via `?key=` query parameter

**New implementation with Vertex AI SDK**:
- Official SDK: `cloud.google.com/go/vertexai/genai`
- Service account authentication via `option.WithCredentialsJSON()`
- Native support for system instructions, streaming, safety settings
- Better error handling and retry logic built-in

**Rationale for SDK adoption**:
- ADR 005 chose `net/http` to avoid dependencies, but Vertex AI authentication is complex
- Service account JSON handling requires proper Google auth libraries anyway
- SDK provides typed interfaces and better error messages
- Production-grade retry logic and connection pooling

### 2. Configuration Changes

**Doppler secret changes**:

| Old Secret | New Secret | Type | Default |
|------------|------------|------|---------|
| `GEMINI_API_KEY` | ❌ Remove | - | - |
| - | `GCP_PROJECT_ID` | String | (required) |
| - | `GCP_LOCATION` | String | `us-central1` |
| - | `GCP_SA` | String | (required, already exists) |
| `GEMINI_SYSTEM_PROMPT` | ✅ Keep | String | - |
| `GEMINI_MODEL` | ✅ Keep | String | `gemini-2.0-flash-exp` |

**Config struct** (`internal/leaguewatcher/config.go`):
```go
type Config struct {
    // ... existing fields ...
    
    // Vertex AI configuration
    GCPProjectID       string `yaml:"-"` // GCP_PROJECT_ID
    GCPLocation        string `yaml:"-"` // GCP_LOCATION (default: "us-central1")
    GCPSA              string `yaml:"-"` // GCP_SA (plain JSON)
    GeminiSystemPrompt string `yaml:"-"` // GEMINI_SYSTEM_PROMPT
    GeminiModel        string `yaml:"-"` // GEMINI_MODEL (default: "gemini-2.0-flash-exp")
}
```

**Notes**:
- `GCP_SA` already exists in Doppler (plain JSON format)
- Service account must have `Vertex AI User` IAM role
- Hot-reload via `ConfigManager` continues to work (no architecture changes)

### 3. Implementation Details

**ask.go rewrite** (`internal/leaguewatcher/bot/ask.go`):

```go
func (b *Bot) ask(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, query string) {
    // 1. Get config (hot-reload via ConfigManager)
    cfg := b.configMgr.Get()
    
    // 2. Validate required fields
    if cfg.GCPSA == "" || cfg.GCPProjectID == "" {
        s.ChannelMessageSend(m.ChannelID, "Я не настроен для этого, извини!")
        b.logger.Error("vertex ai not configured")
        return
    }
    
    // 3. Create Vertex AI client with timeout
    ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
    defer cancel()
    
    client, err := genai.NewClient(ctx, cfg.GCPProjectID, cfg.GCPLocation,
        option.WithCredentialsJSON([]byte(cfg.GCPSA)))
    if err != nil {
        s.ChannelMessageSend(m.ChannelID, "Я не настроен для этого, извини!")
        b.logger.Error("failed to create vertex ai client", zap.Error(err))
        return
    }
    defer client.Close()
    
    // 4. Get generative model
    model := client.GenerativeModel(cfg.GeminiModel)
    
    // 5. Set system instruction if provided
    if cfg.GeminiSystemPrompt != "" {
        model.SystemInstruction = &genai.Content{
            Parts: []genai.Part{genai.Text(cfg.GeminiSystemPrompt)},
        }
    }
    
    // 6. Generate content
    resp, err := model.GenerateContent(ctx, genai.Text(query))
    if err != nil {
        s.ChannelMessageSend(m.ChannelID, "У меня проблемы со связью с оракулом.")
        b.logger.Error("failed to generate content", 
            zap.Error(err),
            zap.String("model", cfg.GeminiModel),
            zap.String("project", cfg.GCPProjectID))
        return
    }
    
    // 7. Extract text from response
    if len(resp.Candidates) == 0 {
        s.ChannelMessageSend(m.ChannelID, "Оракул хранит молчание.")
        return
    }
    
    var text string
    for _, part := range resp.Candidates[0].Content.Parts {
        if t, ok := part.(genai.Text); ok {
            text += string(t)
        }
    }
    
    if text == "" {
        s.ChannelMessageSend(m.ChannelID, "Оракул хранит молчание.")
        return
    }
    
    // 8. Send to Discord
    s.ChannelMessageSend(m.ChannelID, text)
}
```

**Key implementation points**:
- Client created per request (SDK handles connection pooling internally)
- 15-second timeout maintained (same as AI Studio)
- Non-streaming response (simplest approach, same UX)
- Model format: `"gemini-2.0-flash-exp"` (SDK handles full path construction)

### 4. Error Handling (Russian Messages)

| Scenario | Russian Message | When |
|----------|----------------|------|
| Missing config | "Я не настроен для этого, извини!" | GCPSA or ProjectID empty |
| Bad credentials | "Я не настроен для этого, извини!" | `genai.NewClient()` error |
| API errors (403/429/timeout) | "У меня проблемы со связью с оракулом." | `GenerateContent()` error |
| Empty response | "Оракул хранит молчание." | No candidates or empty text |

**Logging** (English for debugging):
```go
b.logger.Error("failed to call vertex ai",
    zap.Error(err),
    zap.String("model", cfg.GeminiModel),
    zap.String("project", cfg.GCPProjectID),
    zap.String("location", cfg.GCPLocation))
```

### 5. Testing Strategy

**Integration test** (`internal/leaguewatcher/bot/gemini_integration_test.go`):
- Rename: `TestGeminiIntegration` → `TestVertexAIIntegration`
- Load `GCP_PROJECT_ID`, `GCP_LOCATION`, `GCP_SA` from Doppler
- Use Vertex AI SDK instead of HTTP client
- Keep Russian test query: `"Сколько боссов в бвл?"`
- Skip if Doppler or Vertex config missing

**Manual testing checklist**:
1. Add `GCP_PROJECT_ID` and `GCP_LOCATION` to Doppler
2. Verify `GCP_SA` contains valid service account JSON
3. Update `GEMINI_MODEL` to `"gemini-2.0-flash-exp"`
4. Ensure system prompt includes: `Отвечай всегда на русском языке`
5. Run: `go test ./internal/leaguewatcher/bot -v -run TestVertexAI`
6. Test Discord mention: `@bot привет`
7. Test error handling: Remove `GCP_SA`, verify Russian error
8. Restore config, verify hot-reload works

## Consequences

### Positive

1. **Removes regional restrictions**: Vertex AI available globally, resolves primary blocker
2. **Production-ready infrastructure**: Vertex AI offers SLAs, support, better reliability
3. **Better authentication model**: Service accounts more secure than API keys
4. **Official SDK support**: Typed interfaces, better error handling, built-in retries
5. **Maintains existing UX**: Users see no difference in Discord interaction
6. **Hot-reload preserved**: Config changes still apply without restart
7. **Same model family**: `gemini-2.0-flash-exp` provides equivalent performance

### Negative

1. **Dependency addition**: Adds `cloud.google.com/go/vertexai` and auth libraries (~5MB binary size increase)
2. **GCP account required**: Need GCP project with billing enabled (has free tier)
3. **More complex auth**: Service account JSON more complex than simple API key
4. **SDK learning curve**: Team needs to understand Vertex AI SDK patterns
5. **Cost consideration**: Vertex AI pricing different from AI Studio (though comparable for this usage)

### Neutral

1. **Configuration migration**: One-time Doppler secret update required
2. **Testing overhead**: Integration test requires valid GCP credentials
3. **Model name change**: `gemini-2.0-flash` → `gemini-2.0-flash-exp` (same model, different endpoint)

## Alternatives Considered

### Alternative 1: Continue with AI Studio + VPN

**Approach**: Set up VPN or proxy to bypass regional restrictions.

**Rejected Because**:
- Unreliable for production (VPN can drop, proxy can be slow)
- Additional infrastructure complexity (VPN server, monitoring)
- Doesn't solve root cause (still using restricted service)
- Potential TOS violation with proxy usage

### Alternative 2: Vertex AI with net/http (No SDK)

**Approach**: Keep manual HTTP client pattern from ADR 005, just change endpoint to Vertex AI.

**Rejected Because**:
- Service account authentication is complex (requires google-auth-library anyway)
- Would need to implement OAuth2 token refresh logic manually
- Loses benefit of SDK's error handling and retry logic
- Binary size savings minimal (still need auth libraries)
- Not recommended by Google for production use

### Alternative 3: Different AI Provider (OpenAI, Anthropic)

**Approach**: Switch to OpenAI GPT or Anthropic Claude instead of fixing Gemini.

**Rejected Because**:
- Gemini 2.0 Flash already meets performance/cost requirements
- Would require rewriting prompts and tuning for different model
- ADR 005 already justified Gemini choice
- Doesn't solve regional restriction problem (other providers may have same issue)
- Vertex AI provides path to other Google AI features if needed later

### Alternative 4: Self-hosted LLM (Ollama, LocalAI)

**Approach**: Run local LLM instead of cloud API.

**Rejected Because**:
- Requires GPU infrastructure (expensive)
- Self-hosting adds operational complexity (model updates, scaling)
- Local models (7B-13B) significantly worse quality than Gemini 2.0 Flash
- Latency requirements favor cloud inference

## Future Work

1. **Streaming responses**: Vertex AI SDK supports streaming, could implement for real-time typing effect in Discord
2. **Context retention**: Add conversation history support using `genai.ChatSession`
3. **Function calling**: Leverage Vertex AI function calling for bot commands (e.g., "check my match history")
4. **Safety settings**: Configure safety filters for production (block harmful content)
5. **Model evaluation**: Use Vertex AI evaluation tools to measure response quality
6. **Grounding with search**: Integrate Google Search grounding for factual queries

## References

- **ADR 005**: Initial Gemini AI integration with AI Studio
- **Vertex AI Go SDK**: https://pkg.go.dev/cloud.google.com/go/vertexai/genai
- **Vertex AI Documentation**: https://cloud.google.com/vertex-ai/docs
- **File locations**:
  - `internal/leaguewatcher/bot/ask.go` - Main implementation
  - `internal/leaguewatcher/config.go` - Configuration structure
  - `internal/leaguewatcher/bot/gemini_integration_test.go` - Integration test
  - `go.mod` - Dependencies

## Reviewers

- **Author**: Claude Sonnet 4.5
- **Approver**: v.loginov
- **Date**: 2026-05-09

## Implementation Plan Summary

**Files to modify**:
1. `internal/leaguewatcher/config.go` - Add GCP fields, remove GeminiAPIKey (~20 lines)
2. `internal/leaguewatcher/bot/ask.go` - Complete rewrite with SDK (~150 lines)
3. `internal/leaguewatcher/bot/gemini_integration_test.go` - Update for Vertex AI (~30 lines)
4. `go.mod` - Add dependencies (`go get cloud.google.com/go/vertexai/genai`)
5. `adr/006-vertex-ai-migration.md` - This ADR document

**Pre-deployment checklist**:
- ✅ Add `GCP_PROJECT_ID` to Doppler
- ✅ Add `GCP_LOCATION` to Doppler (or use default)
- ✅ Verify `GCP_SA` exists with valid JSON
- ✅ Update `GEMINI_MODEL` to `gemini-2.0-flash-exp`
- ✅ Verify service account has `Vertex AI User` role
- ✅ Run integration test
- ✅ Test in Discord with Russian queries

**Success criteria**:
- Bot responds to `@mention` in Russian
- No regional restriction errors
- Integration test passes
- Hot-reload continues to work
- All existing bot features unaffected

**Rollback plan**: Revert code, restore `GEMINI_API_KEY` in Doppler (no data migration needed).
