# ADR 007: Nebius Token Factory Migration for LLM-Powered Q&A

## Status

**Accepted** - 2026-10-19

Supersedes [ADR-006](006-vertex-ai-migration.md).

## Context

ADR-006 migrated the `@mention <question>` flow from Google AI Studio to
Vertex AI on GCP to escape regional restrictions. The Vertex AI integration
worked, but carried operational costs that became hard to justify for this
bot's scale:

### Problems with Vertex AI

- **Heavy dependency tree**: `cloud.google.com/go/vertexai` +
  `google.golang.org/api` + `google.golang.org/genai` account for a
  substantial fraction of `go.sum` (~110 lines) and binary size for a
  single chat-completion call.
- **Service-account lifecycle overhead**: `GCP_SA` is a long-lived JSON
  key that has to be rotated, scoped, and stored in Doppler. Token Factory
  uses a single bearer token which is materially simpler to operate.
- **Cost structure**: Vertex AI billing is tied to a GCP project with its
  own quota model; Token Factory's per-token pricing is more
  straightforward for a low-traffic Discord bot.
- **Model flexibility**: Token Factory exposes a rotating catalogue of
  open-weight models (Llama 3.3 70B, DeepSeek-V3, Kimi-K3, Qwen3, …)
  behind one OpenAI-compatible endpoint, so we can switch models with a
  Doppler secret change instead of an SDK migration.

### Requirements

1. **OpenAI-compatible HTTP surface** — simplifies the client to a
   standard `chat.completions` POST.
2. **Single bearer token** for auth, sourced from Doppler like other
   secrets.
3. **Identical Discord UX** — `@BotName <question>` → reply with the
   answer; same Russian error strings as before.
4. **Hot-reload** — model and parameters must be tunable without
   redeploy (existing `ConfigManager` pattern).
5. **No third-party SDK** in Go — the existing `watcher/mobalytics`
   package already demonstrates the stdlib `net/http` pattern.

## Decision

We switch the `!ask` / mention Q&A backend from Vertex AI Gemini to
**Nebius Token Factory** at:

```
POST https://api.tokenfactory.nebius.com/v1/chat/completions
Authorization: Bearer <TOFA_API_KEY>
```

### Implementation

#### 1. Configuration (`internal/leaguewatcher/config.go`)

Remove all Vertex AI / Gemini fields:

```go
// Removed:
GCPSA, GCPProjectID, GCPLocation
GeminiSystemPrompt, GeminiModel
GeminiTemperature, GeminiTopP, GeminiTopK
GeminiMaxOutputTokens, GeminiThinkingBudget
```

Add Token Factory fields:

```go
TofaAPIKey          string  // TOFA_API_KEY (redacted in LogValue)
TofaModel           string  // TOFA_MODEL, default "meta-llama/Llama-3.3-70B-Instruct"
TofaBaseURL         string  // TOFA_BASE_URL, default "https://api.tokenfactory.nebius.com/v1"
TofaSystemPrompt    string  // TOFA_SYSTEM_PROMPT
TofaTemperature     float32 // TOFA_TEMPERATURE, default 0.4
TofaTopP            float32 // TOFA_TOP_P, default 0.95
TofaMaxOutputTokens int32   // TOFA_MAX_OUTPUT_TOKENS, default 2048
```

`top_k` and `thinking_budget` are **dropped** — they are not part of the
OpenAI chat.completions schema and Token Factory does not honour them
via the standard request body.

#### 2. HTTP client (`internal/leaguewatcher/bot/ask.go`)

- `generateWithVertexAI` replaced by `generateWithTokenFactory`.
- stdlib `net/http` with a shared `*http.Client` (safe for concurrent
  use); per-call 15-second timeout is enforced by the request `context`
  inside `Bot.ask`.
- Strongly typed `tofaChatRequest` / `tofaChatResponse` cover the OpenAI
  subset we consume: `model`, `messages[]`, `temperature`, `top_p`,
  `max_tokens` out; `choices[0].message.content` plus the optional
  `error` envelope in.
- Long non-2xx response bodies are truncated to 512 bytes before being
  embedded in the returned error to keep logs readable.

#### 3. Error signalling

- Sentinel `ErrLLMNotConfigured` (introduced in the preparatory
  refactor commit) continues to mark the "API key or model missing"
  case. `Bot.ask` matches it with `errors.Is` and replies
  *"Я не настроен для этого, извини!"*.
- All other failures (network, non-2xx, parse errors) map to
  *"У меня проблемы со связью с оракулом."* and a structured error log
  with `model` and `base_url` fields.

#### 4. `Config.LogValue()`

`TofaAPIKey` is logged as `***REDACTED***`. `TofaModel`, `TofaBaseURL`
and the numeric parameters are safe to log and useful when debugging
model behaviour.

#### 5. `Config.IsValid()`

`TofaAPIKey` / `TofaModel` are **not** required — the Q&A flow is an
optional feature, and degrading to the Russian "not configured" reply
matches existing behaviour.

### What did NOT change

- Discord message routing (`bot.go:cmd()`), mention detection, or the
  list of `!` commands.
- `Bot.ask` and `Bot` constructor signatures.
- `cmd/leaguewatcher/main.go` — no wiring changes; the LLM config is
  read from `ConfigManager.Get()` at call time as before.
- Watcher, Mobalytics, Khaleesi, pidor, repository code.

## Consequences

### Positive

- **Dependency tree shrinks**: `go mod tidy` removes
  `cloud.google.com/go/vertexai`, `google.golang.org/api`,
  `google.golang.org/genai` and ~110 transitive `go.sum` entries.
  Faster CI builds, smaller binary.
- **Simpler secret hygiene**: one bearer token replaces a GCP service
  account JSON. Rotation = update one Doppler secret.
- **Hot-swappable models**: switching from `Llama-3.3-70B-Instruct`
  to `moonshotai/Kimi-K3` (or any other listed model) is a Doppler
  edit + at most one auto-reload cycle (5 min default). Verified
  against `moonshotai/Kimi-K3` in production config.
- **Consistent with codebase style**: the stdlib `net/http` pattern
  mirrors `watcher/mobalytics/client.go`; no new conceptual surface.
- **Test seams**: `httptest.NewServer` covers auth header, schema,
  system-prompt toggling, error paths — something the Vertex SDK made
  awkward without a fake gRPC server.

### Negative

- **Single provider / single region**: if Token Factory has an outage,
  the Q&A path is degraded until operators intervene. No fallback to
  Gemini. (Acceptable: feature is non-critical.)
- **Loss of `top_k` and `thinking_budget`**: tooling around those
  parameters is gone. They were fine-tuning knobs (defaults 40 and 300
  respectively) and are not commonly adjusted via Doppler today, so
  this is informational rather than operational.
- **Response shape differs slightly**: OpenAI `choices[0].message.content`
  vs Gemini `Candidates[0].Content.Parts[]`. We only ever consumed a
  single text part, so the migration is loss-less for our use case.
- **No streaming**: same as before, but worth noting that
  `chat.completions` supports streaming and we have chosen not to use
  it. Adding it later requires message-edit logic on the Discord side
  (separate ADR).

### Neutral

- The Russian user-facing strings are unchanged, so end users see no
  difference unless they watch bot logs.
- Doppler retains the `GCP_*` and `GEMINI_*` secrets in `prd` for
  rollback; cleanup is a follow-up manual step once Token Factory is
  proven stable.

## Alternatives Considered

### 1. Keep Vertex AI, trim dependencies manually

Rejected: the heavy dep graph is the only realistic way to call Vertex
from Go. Hand-rolling a Vertex-shaped HTTP client (with signed JWT
exchange) would be **more** code, not less.

### 2. Google AI Studio again (ADR-005 path)

Rejected for the same reason it was rejected in ADR-006: regional
blocking makes it unusable from production.

### 3. Use a Go OpenAI SDK (`sashabaranov/go-openai`, `openai/openai-go`)

Rejected: adds a third-party dependency for ~80 LoC worth of JSON
marshalling. The Mobalytics package already proves the stdlib pattern
works fine. We may revisit if function-calling, structured outputs or
streaming become requirements — at that point an SDK pays for itself.

### 4. Side-by-side providers (feature-flag in Doppler)

Rejected: doubles the configuration surface for marginal benefit.
Rollback is a one-commit `git revert` and Doppler still has the Gemini
secrets, so flipping back is cheap.

### 5. Anthropic / OpenRouter / Groq

Considered and rejected for pricing and ops reasons:
- Anthropic: EUR billing, no EU region availability at the tier we
  needed.
- OpenRouter: extra hop, opaque rate-limit story for the free tier.
- Groq: generous free tier but model catalogue at the time was
  thinner for Russian-language workloads than Token Factory.

## Future Work

- **Streaming replies**: use `stream: true` + chunked Discord message
  edits for live-typing effect. Requires rate-limit-aware message
  editing on the Discord side.
- **Conversation memory**: persist a short history per Discord channel
  to support follow-up questions.
- **Per-channel model override**: expose per-channel `TOFA_MODEL`
  selection if usage grows beyond the current single-server scope.
- **Cleanup pass**: delete `GCP_*` and `GEMINI_*` secrets from Doppler
  once Token Factory has been stable for ~2 weeks. Remove legacy code
  references in the same PR.
- **Rate-limit-aware retry**: 15 s ctx timeout is currently the only
  guard. If 429s appear in production, add a small exponential backoff.

## References

- Plan: `PLAN-nebius-token-factory.md` (this branch)
- Migration commit: `feat: switch !ask backend to Nebius Token Factory`
- Code:
  - `internal/leaguewatcher/bot/ask.go` — LLM call
  - `internal/leaguewatcher/bot/ask_test.go` — fake-server unit tests
  - `internal/leaguewatcher/bot/tofa_integration_test.go` — Doppler-driven smoke test
  - `internal/leaguewatcher/config.go` — `Tofa*` config + `Reload` parsing
- Doppler project `leaguewatcherbot`, config `prd`:
  - `TOFA_API_KEY`, `TOFA_MODEL=moonshotai/Kimi-K3`,
    `TOFA_BASE_URL=https://api.tokenfactory.nebius.com/v1`,
    `TOFA_SYSTEM_PROMPT`, `TOFA_TEMPERATURE`, `TOFA_TOP_P`,
    `TOFA_MAX_OUTPUT_TOKENS`
- External: <https://docs.tokenfactory.nebius.com/> (OpenAI-compatible
  API reference)

## Reviewers

- Author: @cauchy2384
- Approved: @cauchy2384
- Date: 2026-10-19

## Changelog

| Date | Change |
|------|--------|
| 2026-10-19 | Initial version, marked Accepted. Supersedes ADR-006. |
