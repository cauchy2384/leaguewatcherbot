# ARCHITECTURE.md

**Project**: League Watcher Bot  
**Repository**: [leaguewatcherbot](https://github.com/username/leaguewatcherbot)  
**Purpose**: Discord bot for monitoring League of Legends player matches and posting updates to a Discord channel  
**Last Updated**: 2026-05-04

---

## 1. Project Structure

```
leaguewatcherbot/
├── cmd/
│   └── leaguewatcher/          # Application entry point
│       └── main.go             # Initializes config, watcher, and bot; manages lifecycle
│
├── internal/
│   ├── khaleesi/              # Text mutation/replacement engine (Easter egg feature)
│   │   └── khaleesi.go        # Regex-based text transformations
│   │
│   └── leaguewatcher/         # Core application logic
│       ├── config.go          # Configuration structure and validation
│       ├── player.go          # Player data model
│       ├── match.go           # Match data model and URL generation
│       ├── event.go           # Event logging structure
│       ├── champion.go        # Champion data model
│       │
│       ├── bot/               # Discord bot implementation
│       │   ├── bot.go         # Main bot logic, command routing, match notifications
│       │   ├── track.go       # Channel tracking system (enable/disable notifications)
│       │   ├── pidor.go       # "Pidor of the day" game mechanics
│       │   ├── info.go        # Info command implementation
│       │   ├── khaleesi.go    # Khaleesi integration for text mutations
│       │   └── repository/    # Data persistence layer
│       │       ├── pidor.go   # Pidor statistics JSON storage
│       │       └── log.go     # Event logging to JSON
│       │
│       └── watcher/           # Match detection and monitoring system
│           ├── watcher.go     # Main watcher logic, periodic polling
│           ├── mobalytics/    # External API client
│           │   └── client.go  # GraphQL queries for matches and champion data
│           └── repository/    # State management
│               └── match.go   # In-memory match cache for deduplication
│
├── tools/
│   ├── migrator/              # Standalone SQL migration runner (ADR-010)
│   ├── pidors-backfill/       # One-shot importer: pidors.json → pidor_rolls (ADR-011)
│   └── pidors2csv/            # Utility to export pidor statistics to CSV
│
├── config.yaml                # Configuration file (poll period, players, channel ID)
├── pidors.json               # Persistent pidor game statistics (generated at runtime)
└── log.json                  # Event audit log (generated at runtime)
```

**Architectural Layers**:
- **Entry Point** (`cmd/`): Application initialization and lifecycle management
- **Core Logic** (`internal/leaguewatcher/`): Domain models and business logic
- **External Integrations** (`watcher/mobalytics/`, `bot/`): API clients for Mobalytics and Discord
- **Persistence** (`bot/repository/`, `watcher/repository/`): Data storage abstractions
- **Utilities** (`tools/`): Supporting scripts and tools

---

## 2. High-Level System Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                          League Watcher Bot                          │
├─────────────────────────────────────────────────────────────────────┤
│                                                                       │
│  ┌──────────────┐                                 ┌──────────────┐  │
│  │   Config     │────────────────────────────────▶│     Main     │  │
│  │  (YAML +     │                                 │   (Startup)  │  │
│  │   EnvVars)   │                                 └──────┬───────┘  │
│  └──────────────┘                                        │          │
│                                                           │          │
│                          ┌────────────────────────────────┴────┐    │
│                          ▼                                     ▼    │
│                  ┌───────────────┐                    ┌─────────────┐
│                  │    Watcher    │                    │     Bot     │
│                  │  (Goroutine)  │                    │ (Goroutine) │
│                  └───────┬───────┘                    └──────┬──────┘
│                          │                                   │       │
│                          │  Poll every 1min                  │       │
│                          ▼                                   │       │
│                  ┌───────────────┐      Match Channel        │       │
│                  │  Mobalytics   │──────────────────────────▶│       │
│                  │  GraphQL API  │   (New matches detected)  │       │
│                  └───────────────┘                            │       │
│                          │                                    │       │
│                          │ Champions sync                     │       │
│                          │                                    │       │
│                          ▼                                    ▼       │
│                  ┌───────────────┐                    ┌──────────────┐
│                  │  Match Cache  │                    │ Discord API  │
│                  │ (In-Memory)   │                    │  (discordgo) │
│                  └───────────────┘                    └──────┬───────┘
│                                                               │        │
│                                                               │        │
│                                                       ┌───────▼──────┐ │
│                                                       │   JSON Files │ │
│                                                       │  (pidors.json│ │
│                                                       │   log.json)  │ │
│                                                       └──────────────┘ │
└─────────────────────────────────────────────────────────────────────┘

External Systems:
  - Mobalytics API (GraphQL): Match history and champion data
  - Discord API (WebSocket + REST): Bot commands and message posting
```

**Data Flow**:
1. Watcher polls Mobalytics API every 1 minute for configured players
2. New matches are detected via in-memory cache comparison
3. Matches sent through channel to Bot
4. Bot formats matches and posts to tracked Discord channels
5. Bot listens for Discord messages and executes commands
6. Bot persists statistics and logs to JSON files

---

## 3. Core Components

### 3.1 Watcher
**Location**: `internal/leaguewatcher/watcher/`

**Responsibilities**:
- Periodically poll Mobalytics GraphQL API for player match history
- Detect new matches by comparing against in-memory cache
- Filter matches based on age (configurable `played_gap`)
- Send detected matches to Bot via Go channel
- Concurrent polling of multiple players using `errgroup`

**Key Technologies**:
- `github.com/hasura/go-graphql-client` - GraphQL client
- `golang.org/x/sync/errgroup` - Concurrent goroutine management
- Go channels for inter-component communication

**Deployment**: Runs as a background goroutine, started by `main.go`

### 3.2 Bot
**Location**: `internal/leaguewatcher/bot/`

**Responsibilities**:
- Connect to Discord and listen for messages
- Route commands (`!track`, `!untrack`, `!pidor`, `!pidorstats`, etc.)
- Format and post match notifications to tracked channels
- Manage "pidor of the day" game with persistent statistics
- Log all command executions for audit trail
- Apply Khaleesi text mutations to user messages (Easter egg)

**Key Technologies**:
- `github.com/bwmarrin/discordgo` - Discord API client library
- `github.com/kyokomi/emoji/v2` - Emoji rendering in Discord messages
- Go channels for receiving matches from Watcher

**Deployment**: Runs as a background goroutine, started by `main.go`

### 3.3 Mobalytics Client
**Location**: `internal/leaguewatcher/watcher/mobalytics/`

**Responsibilities**:
- Execute GraphQL queries against Mobalytics API
- Fetch recent matches for a given summoner (player)
- Synchronize champion data (ID to name mapping)
- Support WebSocket subscription for profile refresh status

**Key Technologies**:
- `github.com/hasura/go-graphql-client` - GraphQL operations
- HTTP/GraphQL over HTTPS to `mobalytics.gg`

**Deployment**: Used by Watcher component; no separate lifecycle

### 3.4 Khaleesi
**Location**: `internal/khaleesi/`

**Responsibilities**:
- Apply regex-based text transformations to user messages
- Easter egg feature for humorous text mutations
- Configurable replacement rules

**Key Technologies**:
- Go standard library `regexp` package

**Deployment**: Invoked by Bot when processing Discord messages

---

## 4. Data Stores

### 4.1 pidors.json
**Type**: JSON file (persistent)

**Purpose**: Store "pidor of the day" game statistics

**Data Stored**:
- Player name and selection count
- Last selection timestamp (prevents duplicate selection same day)

**Schema** (conceptual):
```json
{
  "stats": [
    {"name": "UserName", "count": 5}
  ],
  "last": {
    "timestamp": "2026-05-04T10:00:00Z",
    "name": "UserName"
  }
}
```

### 4.2 log.json
**Type**: JSON file (persistent)

**Purpose**: Audit log of all bot commands executed

**Data Stored**:
- Timestamp of command execution
- Action/command name
- User who executed the command

**Schema** (conceptual):
```json
[
  {
    "timestamp": "2026-05-04T10:00:00Z",
    "action": "pidor",
    "user": "DiscordUsername#1234"
  }
]
```

### 4.3 In-Memory Match Cache
**Type**: In-memory data structure (ephemeral)

**Purpose**: Deduplicate match notifications to prevent duplicate posts

**Data Stored**:
- Match IDs of recently processed matches
- Cleared on application restart

### 4.4 pidor_rolls (Neon Postgres)
**Type**: Relational table (persistent, future canonical store)

**Purpose**: Durable roll-event history and aggregate store for the pidor game, replacing `pidors.json` in a follow-up release

**Schema** (see `internal/leaguewatcher/db/migrations/0002_pidor_rolls.up.sql`):
- `id` BIGSERIAL PK
- `channel_id`, `user_id`, `user_name` TEXT (Discord snowflakes as strings; username is a snapshot at roll time)
- `rolled_on` DATE — canonical pidor day, computed by the application in `Europe/Moscow`
- `rolled_at` TIMESTAMPTZ — audit insert timestamp
- `times_rolled` INTEGER ≥ 1 — 1 for live rolls, N for backfilled aggregates

**Constraint**: `UNIQUE (channel_id, rolled_on)` — at most one winner per channel per day enforced at the storage layer (defence-in-depth against double-roll races).

**Status**: Schema landed (migration 0002, ADR-011). Historical
aggregates can be imported via `tools/pidors-backfill` (one-shot CLI,
idempotent via `ON CONFLICT DO NOTHING` + deterministic synthetic
dates per user within each channel). Bot code **does not yet read or
write** this table — the consumer PR is tracked separately. See
`adr/011-pidor-rolls-table.md` for the full schema and backfill
rationale.

---

## 5. External Integrations/APIs

### 5.1 Discord API
**Service**: Discord (via `discord.com` WebSocket and REST APIs)

**Function**: 
- Bot authentication and presence
- Receive user messages and commands
- Post match notifications and command responses
- Manage channel subscriptions (track/untrack)

**Integration Method**:
- Library: `github.com/bwmarrin/discordgo`
- Authentication: Bot token via environment variable `BOT_DISCORD_TOKEN`
- Protocols: WebSocket (Gateway) for real-time events, REST for API calls

### 5.2 Mobalytics GraphQL API
**Service**: Mobalytics (`mobalytics.gg` GraphQL endpoint)

**Function**:
- Fetch recent match history for League of Legends players
- Retrieve champion data (ID to name mapping)
- Monitor profile refresh status

**Integration Method**:
- Library: `github.com/hasura/go-graphql-client`
- Authentication: None (public API)
- Protocol: GraphQL over HTTPS

---

## 6. Deployment & Infrastructure

**Deployment Model**: Single binary executable (standalone process)

**Cloud Provider**: None (can run on any system supporting Go binaries)

**Key Services**:
- **Runtime**: Go 1.22.0+ binary
- **Configuration Management**: `config.yaml` file + environment variables
- **Logging**: Structured logs via `go.uber.org/zap` (stdout/stderr)

**CI/CD Pipeline**: 
- Git repository on GitHub
- No automated deployment pipeline detected (manual deployment)

**Monitoring & Observability**:
- Structured logging with Zap (development mode)
- Log levels: Info, Error
- No external monitoring services integrated

**Scaling**: 
- Single instance deployment (no horizontal scaling)
- Stateless design allows restart without data loss (except in-memory cache)

---

## 7. Security Considerations

### Authentication & Authorization
- **Discord Bot Token**: Stored in environment variable `BOT_DISCORD_TOKEN`, never in code or config files
- **Bot Owner Authorization**: Certain commands restricted to user specified in `BOT_OWNER_ID` environment variable
- **Discord OAuth**: User authentication handled by Discord platform (via discordgo library)

### Data Protection
- **No sensitive data in config.yaml**: Only player summoner names and Discord channel IDs
- **No encryption at rest**: JSON files contain non-sensitive game statistics
- **No encryption in transit** (internal): Communication between Watcher and Bot via in-process Go channels

### Security Tools
- None explicitly configured

### Known Considerations
- Bot token must be kept secret (Discord platform security)
- `BOT_OWNER_ID` determines command authorization scope
- No rate limiting on bot commands (relies on Discord's built-in rate limits)

---

## 8. Development & Testing Environment

### Local Setup
1. Install Go 1.22.0 or higher
2. Clone repository
3. Create `config.yaml` with player configuration and Discord channel ID
4. Set environment variables:
   - `BOT_DISCORD_TOKEN`: Discord bot token
   - `BOT_OWNER_ID`: Discord username for bot owner
   - `DOPPLER_TOKEN`: Doppler service token (configuration is loaded from Doppler secrets)

   Optional Doppler-managed secrets (Neon PostgreSQL, per ADR-009):
   - `DATABASE_URL`: direct Neon connection string (migrations/tools)
   - `DATABASE_URL_POOLED`: pooled Neon connection string (runtime traffic)
   - `NEON_AUTH_BASE_URL`: Neon Auth base URL (reachability check only; auth flows deferred)
5. Run: `go run cmd/leaguewatcher/main.go`

### Schema Migrations (per ADR-010)

SQL schema changes are applied via `tools/migrator`, a standalone CLI:

```bash
doppler run -- go run tools/migrator/main.go status   # what's applied
# doppler run -- go run tools/migrator/main.go up     # apply pending
# doppler run -- go run tools/migrator/main.go down   # roll back one
```

Migration files live in `internal/leaguewatcher/db/migrations/` as
`{NNNN}_{snake_name}.{up|down}.sql` pairs and are compiled into the
binary via `embed.FS`. The `schema_migrations` table (created by
migration `0001`) records which version was applied and when.

**Rules:**
- Never edit a migration file once it has been merged to `main`.
- The bot does **not** auto-apply migrations on startup; operators run
  the migrator explicitly.
- The migrator uses `DATABASE_URL` (direct endpoint), never
  `DATABASE_URL_POOLED` — advisory locks require session affinity.

### Testing Framework
- **Framework**: Go standard `testing` package
- **Assertions**: `github.com/stretchr/testify/assert` and `testify/require`
- **Test Execution**: `go test ./...`

### Code Quality Tools
- **Linting**: Go standard toolchain (`go fmt`, `go vet`)
- **Formatting**: `gofmt` (standard Go formatting)
- **No additional linters configured** (gofumpt, golangci-lint, etc.)

### Test Coverage
- Unit tests present for core components:
  - `bot_test.go`, `watcher_test.go`
  - `khaleesi_test.go`
  - `match_test.go`, `config_test.go`
- No integration tests or end-to-end tests detected

---

## 9. Future Considerations/Roadmap

### Known Technical Debt
- **JSON file persistence**: Simple but not scalable; consider migration to proper database (SQLite, PostgreSQL)
- **Polling-based match detection**: Inefficient; webhook-based approach would reduce API calls and improve latency

### Planned Major Changes
- **Database migration**: Replace JSON files with relational database for better querying and scalability
- **Webhook integration**: Switch from polling to event-driven architecture if Mobalytics supports webhooks
- **Discord slash commands**: Modernize bot interface with Discord's slash command system
- **Champion statistics**: Add tracking of champion win rates, most played, etc.
- **Trending detection**: Identify and report on winning/losing streaks

### Architectural Evolution
- Potential microservice split: Separate Watcher and Bot into independent services
- API layer: Expose match data via REST/GraphQL for other consumers
- Persistent storage: Introduce proper database for match history and analytics

---

## 10. Glossary/Acronyms

| Term | Definition |
|------|------------|
| **KDA** | Kills/Deaths/Assists ratio - a performance metric in League of Legends |
| **LP** | League Points - ranked progression currency in League of Legends |
| **Pidor** | Joke game feature that randomly selects a Discord user daily (Russian slang) |
| **Mobalytics** | Third-party analytics platform for League of Legends with player statistics and match history |
| **Summoner** | League of Legends player account identifier (summoner name + tag) |
| **Tag** | Server/region identifier for summoner names (e.g., `euw` for Europe West) |
| **Champion** | Playable character in League of Legends |
| **Queue Type** | Game mode category (ranked, normal, ARAM, etc.) |
| **GraphQL** | Query language for APIs used by Mobalytics |
| **discordgo** | Go library for interacting with Discord API |
| **Zap** | Structured logging library for Go (Uber's logger) |

---

## Additional Resources

- **Architecture Standard**: [architecture.md](https://architecture.md/)
- **Discord Developer Portal**: [discord.com/developers](https://discord.com/developers)
- **Mobalytics**: [mobalytics.gg](https://mobalytics.gg)
- **Go Documentation**: [go.dev](https://go.dev)
