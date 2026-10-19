# ADR 008: Tavily Search Grounding for LLM Q&A

## Status

**Accepted** - 2026-10-19

Builds on [ADR-007](007-nebius-token-factory-migration.md).

## Context

After ADR-007 the `@mention <question>` flow calls Nebius Token Factory
directly with the user's question and the system prompt from Doppler.
The system prompt scopes the bot to *WoW Ascension Bronzebeard* — a
niche private-server scene — and other computer games. Two accuracy
issues with this setup:

- **Hallucination on niche topics**: open-weight models (incl.
  `moonshotai/Kimi-K3`, our production default) were trained on
  relatively little Ascension-specific data. The model frequently
  confuses Ascension mechanics, item stats, and raid details with
  retail or Classic Era.
- **No freshness**: patches land on Ascension every few weeks, but the
  model has a fixed training cutoff. There is no mechanism to fetch
  current facts at answer time.

Both are the canonical use case for **search-grounded generation**
(RAG-lite): call a search API per query, inject the top snippets as
context, and let the LLM synthesize.

## Decision

Add a single Tavily Search call between mention detection and the LLM
call. Top-N snippets are appended to the existing system prompt as a
Russian-language numbered block:

```
Контекст из поиска (используй для ответа, но не упоминай явно):
1. <title> — <url>
<content>
...
```

The user message stays verbatim. The LLM reply is unchanged in shape
and Discord-side handling.

### Why Tavily (and not the alternatives)

| Provider | Reason rejected |
|----------|-----------------|
| Perplexity Search API | Better answer synthesis but we already pay for an LLM; running two synthesis layers doubles cost. Perplexity's US billing also awkward from EUR. |
| Brave Search API | Strong general web index, but Q&A-shaped snippet quality is worse than Tavily's LLM-extracted `content` field for our queries. |
| Serper (Google wrapper) | Highest result quality in benchmarks, but pricing is per-call (not credits), and the Ascension topic specifically returned more relevant snippets from Tavily in ad-hoc comparison. |
| Google Custom Search Engine | 100 free queries/day is tight, requires maintaining a Google Cloud project alongside the existing infrastructure, and the result shape forces us to fetch + extract page content ourselves. |
| Direct crawling (Tavily Crawl / Fandom API) | Higher-quality data but introduces a scheduled pipeline, storage for the crawled corpus, and operational complexity disproportionate to single-server traffic. Listed as Future Work. |

Tavily was chosen because:

1. The `content` field of each result is already a cleaned, ~400-token
   snippet — exactly the right size for our prompt budget without
   further processing.
2. Single bearer-token auth, single endpoint, OpenAI-style request
   shape — minimal code surface.
3. Free tier (~1 000 credits/month) covers projected traffic
   (5–20 mentions/day ⇒ 150–600 credits/mo).
4. `search_depth=basic` finishes in well under the 5 s sub-budget we
   reserved for it.

### Endpoint parameters

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `search_depth` | `basic` (default; Doppler-tunable) | `advanced` ≈ 5–10 s, blows the 15 s outer budget. |
| `topic` | `general` | Ascension facts aren't news-shaped. |
| `max_results` | `3` (default; Doppler-tunable) | 3 × ~400 tokens ≈ 1.2k tokens of context; small for Kimi-K3, leaves room for the existing system prompt. |
| `include_answer` | `false` | The LLM synthesizes; Tavily's pre-cooked answer adds cost and bypasses our system prompt's tone/scope rules. |
| `include_raw_content` | `false` | Avoids prompt-injection and prompt-size explosions. |
| `include_domains` / `exclude_domains` | empty (Doppler-tunable CSV) | Optional override; shipping empty preserves recall for Reddit/community sources. |
| `time_range` | empty | Ascension facts are mostly timeless; recency filtering would harm recall. |
| `auto_parameters` | `false` | Pin everything for determinism and testability. |

Auth: `Authorization: Bearer <TAVILY_API_KEY>` header (never in URL,
never in body).

### Error semantics: search is additive, never blocking

Any `searchTavily` failure (timeout, non-2xx, parse error, missing
key) is logged and the flow falls back to LLM-only:

| Failure | Behaviour |
|---------|-----------|
| `TAVILY_API_KEY` missing | `ErrTavilyNotConfigured`, debug-log, LLM-only |
| HTTP non-2xx | Warn-log with status, LLM-only |
| Network / timeout | Warn-log, LLM-only |
| Empty `results[]` | LLM-only (not an error) |

The bot **always** answers within the outer 15 s budget. Detection of
a Tavily outage therefore never translates into a user-visible error.

### Code organisation

- `internal/leaguewatcher/bot/tavily.go` — provider client, mirrors
  the `ask.go` one-file-per-provider pattern. stdlib `net/http` (no
  third-party SDK).
- `Bot.ask` orchestrates: search → `buildMessages(query, basePrompt,
  results)` → `generateWithTokenFactory(ctx, cfg, messages)`. The
  message list is composed in `Bot.ask` (not in the LLM helper) so
  new context sources don't grow the helper's signature.
- Preparatory refactor commit moves `generateWithTokenFactory` to
  accept `[]tofaMessage` before the feature commit plugs in search.

### Configuration

New optional Doppler secrets for `leaguewatcherbot/prd`:

```
TAVILY_API_KEY                # Activates the feature; redacted
TAVILY_BASE_URL               # Default https://api.tavily.com
TAVILY_SEARCH_DEPTH           # Default basic
TAVILY_TOPIC                  # Default general
TAVILY_MAX_RESULTS            # Default 3
TAVILY_TIMEOUT_SECONDS        # Default 5
TAVILY_INCLUDE_DOMAINS        # CSV; default empty
TAVILY_EXCLUDE_DOMAINS        # CSV; default empty
```

Hot-reload works through the existing `ConfigManager` 5-minute cycle:
tuning `TAVILY_MAX_RESULTS` requires no redeploy.

## Consequences

### Positive

- **Accuracy on niche topics**: Ascension facts, items, and raid
  strategies are now grounded in fresh web results rather than model
  priors — visible improvement on the smoke query "WoW Ascension
  Bronzebeard Onyxia strategy" (5 relevant hits incl. Ascension wiki
  and YouTube).
- **Freshness**: patch notes surface immediately through search
  instead of waiting for the next model retraining.
- **Zero-cost graceful degradation**: disabling is removing a single
  Doppler secret; the bot never breaks, it just answers from prior
  knowledge.
- **Test seams**: all Tavily behaviours unit-testable with
  `httptest.Server` — same pattern as Token Factory.
- **No new dependencies**: stdlib `net/http` only.

### Negative

- **Latency**: p50 mention→reply latency grows by the search RTT
  (~0.5–2 s observed in integration tests). Acceptable for a chat-bot
  UX.
- **Cost**: 1 credit per mention (basic depth). At current traffic
  comfortably inside the free tier; if mentions grow 10×, we'll need
  a paid plan or a cache.
- **Noise floor**: shaky search results (spam SEO domains) can leak
  into the prompt and bias the answer. `TAVILY_EXCLUDE_DOMAINS`
  exists as the mitigant; we accept the risk while traffic is low.
- **Cross-request coupling**: a slow search call holds buffered
  Discord reaction; even with the sub-budget, a p99 LLM call after
  a p99 search call could approach the 15 s wall clock timeout and
  return an error string to the user. Mitigation: tune
  `TAVILY_TIMEOUT_SECONDS` down via Doppler without redeploy.

### Neutral

- Existing user-facing strings unchanged: `Я не настроен…`,
  `У меня проблемы со связью…`, `Оракул хранит молчание.` behave as
  before regardless of search state.
- The `top_k` and `thinking_budget` TOFA options are still unused;
  Tavily grounding affects the *prompt*, not the decoding parameters.

## Alternatives Considered

### 1. Function-calling / tool-use in the LLM

Token Factory supports OpenAI `tools` on some models. We considered
defining a `search(query)` tool and letting the model decide to call
it. Rejected for v1: requires models that support tool use reliably
(Kimi-K3's OpenAI-compat tools path is shaky at time of writing),
adds a round-trip (search → tool result → second LLM call) that
easily doubles latency, and is harder to make deterministic in tests
than a fixed pre-step. Easy to revisit once v1 is stable.

### 2. Vector store with periodic Ascension wiki crawl

Higher-quality grounding, but introduces: a scheduled crawler, an
embedding model, a vector DB, retention policy, and a freshness
story. Disproportionate to single-server Discord traffic. Listed as
Future Work in §12 of the plan.

### 3. Always-include-domains pinning

Tempting to force `project-ascension.fandom.com,ascension.gg,wowhead.com`
in code. Rejected: too narrow, loses Reddit/YouTube/community answers
that are often the best source on Ascension's rapidly-changing
meta. Shipping empty; will tune via Doppler if observed quality
demands it.

### 4. Use both Tavily AND a backup search (dual-ground)

Doubles cost and latency for marginal quality gain; rejected.

### 5. Embed search snippets as a separate `system` message

Two system messages work on OpenAI but compatibility across
OpenAI-compatible providers is shakier; Kimi-K3 also treats a single
larger system message better. Single concatenated system message it is.

## Future Work

- **Semantic cache**: `(query_embedding) → (results, ttl)` to save
  Tavily credits on repeated/similar questions. Needs an embedding
  model and a small SQLite or Redis store.
- **!askraw debug command**: show raw Tavily hits without LLM
  synthesis. Useful for tuning include/exclude domains.
- **Two-stage flow**: classifier decides if the query is on-topic
  before paying the search + LLM credits; would cut costs on
  off-topic mentions.
- **Tavily Extract / Crawl** as a scheduled job to build a
  project-local Ascension wiki mirror, replacing per-query search.
- **Tool-using LLM**: replace the fixed pre-step with `tools` on a
  model that supports it well.
- **Per-channel domain pinning**: different Discord channels get
  different `include_domains` (e.g. `#wow-ascension` vs `#lol`).

## References

- Plan: `PLAN-tavily-search.md` (working branch only)
- Preceding ADR: [007-nebius-token-factory-migration.md](007-nebius-token-factory-migration.md)
- Implementation commits on branch `feat/tavily-search-integration`:
  - `refactor: pass []tofaMessage into generateWithTokenFactory`
  - `feat: ground !ask answers with Tavily search results`
- Code:
  - `internal/leaguewatcher/bot/tavily.go` — Tavily client
  - `internal/leaguewatcher/bot/ask.go` — `Bot.ask`, `buildMessages`
  - `internal/leaguewatcher/config.go` — `Tavily*` fields + `Reload`
- Doppler project `leaguewatcherbot`, config `prd`:
  - `TAVILY_API_KEY` (required), `TAVILY_*` (optional overrides)
- External:
  - <https://docs.tavily.com/api-reference/endpoint/search>

## Reviewers

- Author: @cauchy2384
- Approved: @cauchy2384
- Date: 2026-10-19

## Changelog

| Date | Change |
|------|--------|
| 2026-10-19 | Initial version, marked Accepted. |
