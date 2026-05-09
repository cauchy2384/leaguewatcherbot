# AGENTS.md

AI coding agent instructions for League Watcher Bot. This file provides precise, executable commands and critical constraints for working on this codebase.

For system architecture, see [ARCHITECTURE.md](ARCHITECTURE.md).  
For human contributor guidelines, see [CONTRIBUTING.md](CONTRIBUTING.md).

---

## Project Overview

**Type**: Discord bot for monitoring League of Legends player matches  
**Language**: Go 1.22.0+  
**Architecture**: Single-process event-driven bot with Discord and Mobalytics GraphQL integration  
**Persistence**: JSON files (`pidors.json`, `log.json`)  
**Deployment**: Standalone binary, single instance only

---

## Quick Start

```bash
# Prerequisites: Go 1.22.0+, Discord bot token
go mod download

# Create config.yaml in repository root (see config.yaml for example)
# Set required environment variables
export BOT_DISCORD_TOKEN="your-discord-bot-token-here"
export BOT_OWNER_ID="your-discord-username"

# Run
go run cmd/leaguewatcher/main.go
```

---

## Build and Test Commands

### Build
```bash
# Development build
go build -o leaguewatcherbot cmd/leaguewatcher/main.go

# Production build (optional flags)
go build -ldflags="-s -w" -o leaguewatcherbot cmd/leaguewatcher/main.go
```

### Test
```bash
go test ./...                           # Run all tests
go test -v ./...                        # Verbose output
go test -run TestMatchURL ./...         # Run specific test
go test -cover ./...                    # With coverage report
go test ./internal/leaguewatcher/bot    # Test specific package
```

### Format and Lint
```bash
go fmt ./...    # Format all Go files (REQUIRED before commit)
go vet ./...    # Run Go linter (REQUIRED before commit)
```

**CRITICAL**: All tests must pass (`go test ./...`) before creating commits.

---

## Development Guidelines

**CRITICAL**: All development work **MUST** occur within a dedicated feature branch. Never commit directly to the `main` or `master` branches.

1. **Branch Naming**: Use the format `type/description` (e.g., `feat/add-stats-command`, `fix/lp-calculation`).
2. **Pull Requests**: Submit a PR for every change. Ensure the PR title follows Conventional Commits to trigger appropriate CI actions.
3. **Review**: All PRs must be reviewed and pass CI before merging.
4. **Pre-commit Checks**: You **MUST** run `go fmt ./...` and `go vet ./...` and fix all issues before every commit. This is mandatory to pass CI.

---

## Code Organization

### Where to Add New Features

| Feature Type | Location | Pattern |
|--------------|----------|---------|
| **Discord command** | `internal/leaguewatcher/bot/` | Create `commandname.go`, add handler, register in `bot.go:cmd()` |
| **Match processing** | `internal/leaguewatcher/watcher/` | Add logic to `watcher.go` or create new file |
| **Mobalytics API query** | `internal/leaguewatcher/watcher/mobalytics/client.go` | Add GraphQL constant + method |
| **Core data model** | `internal/leaguewatcher/` (root) | Create `modelname.go` (e.g., `match.go`, `player.go`) |
| **Utility/tool** | `tools/` | Create subdirectory with standalone main |

### Directory Structure
```
cmd/leaguewatcher/main.go              # Entry point
internal/leaguewatcher/
  ├── bot/                             # Discord bot
  │   ├── bot.go                       # Main dispatcher, command routing
  │   ├── pidor.go                     # Pidor game commands
  │   ├── track.go                     # Channel tracking (!track, !untrack)
  │   ├── info.go                      # Info commands
  │   ├── khaleesi.go                  # Easter egg integration
  │   └── repository/                  # Data persistence
  │       ├── pidor.go                 # Pidor stats JSON I/O
  │       └── log.go                   # Event logging JSON I/O
  ├── watcher/                         # Match detection
  │   ├── watcher.go                   # Polling loop
  │   ├── mobalytics/                  # API client
  │   │   └── client.go                # GraphQL queries
  │   └── repository/
  │       └── match.go                 # In-memory cache
  └── khaleesi/                        # Text mutation (Easter egg)
      └── khaleesi.go
```

---

## Development Patterns

### Discord Command Pattern

**CRITICAL**: All Discord commands follow this exact pattern.

```go
// In internal/leaguewatcher/bot/commandname.go
func (b *Bot) commandName(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {
    // 1. Check authorization (if owner-only command)
    if m.Author.String() != b.cfg.OwnerID {
        return
    }
    
    // 2. Execute command logic
    result := doSomething()
    
    // 3. Send response
    s.ChannelMessageSend(m.ChannelID, result)
    
    // 4. Log event
    b.log.AddEvent(leaguewatcher.NewEvent("commandname", m.Author.String()))
    
    // 5. Return silently (NO error return)
}
```

**Register in `bot.go:cmd()` method:**
```go
if strings.EqualFold(args[0], "!commandname") {
    b.commandName(ctx, s, m)
}
```

**Key points**:
- Commands do NOT return errors (they're event handlers)
- Use `strings.EqualFold()` for case-insensitive matching
- Log all commands with `b.log.AddEvent()`
- Owner check: `m.Author.String() == b.cfg.OwnerID` (username string, not ID number)

### Mobalytics API Client Pattern

```go
// 1. Define GraphQL query as constant
const queryNewThing = `
query GetNewThing($region: String!, $name: String!) {
    lol {
        player(region: $region, name: $name) {
            newThing { field1 field2 }
        }
    }
}
`

// 2. Create method
func (c *Client) GetNewThing(ctx context.Context, region, name, tag string) (Result, error) {
    // 3. Use 30-second timeout
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    
    // 4. Construct request with JSON body
    reqBody := map[string]interface{}{
        "query": queryNewThing,
        "variables": map[string]string{
            "region": strings.ToUpper(region),
            "name":   name,
        },
    }
    
    // 5. Make HTTP request, parse response
    // 6. Return typed result
}
```

---

## Testing Requirements

### Preferred Test Pattern (Table-Driven)

```go
func TestFunctionName(t *testing.T) {
    testCases := []struct {
        name     string
        input    InputType
        expected OutputType
    }{
        {
            name:     "descriptive case name",
            input:    InputType{...},
            expected: OutputType{...},
        },
        {
            name:     "edge case",
            input:    InputType{...},
            expected: OutputType{...},
        },
    }
    
    for _, tt := range testCases {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()  // Enable parallel execution
            is := is.New(t)
            
            result := FunctionUnderTest(tt.input)
            
            is.Equal(result, tt.expected)
        })
    }
}
```

### Assertion Libraries

**Preferred**: `github.com/matryer/is` (simple, clean)
```go
is := is.New(t)
is.NoErr(err)           // Assert no error
is.Equal(got, want)     // Assert equality
is.True(condition)      // Assert boolean condition
```

**Alternative**: `github.com/stretchr/testify`
```go
require.NoError(t, err)       // Fail immediately on error
assert.Equal(t, want, got)    // Continue on failure
assert.NotEmpty(t, result)    // Check non-empty
```

### Test Execution

```bash
go test ./...                           # All tests
go test -v ./internal/leaguewatcher/    # Verbose, specific package
go test -run TestMatchURL ./...         # Specific test by name
```

**CRITICAL**: All tests must pass before committing.

---

## Critical Files and Locations

### Configuration File
- **Path**: `config.yaml` (must be in same directory as executable)
- **Format**: YAML
- **Required fields**:
  ```yaml
  poll_period: 1m              # Duration (e.g., 1m, 5m)
  played_gap: 60m              # Duration
  channel_id: 123456789        # Discord channel ID (number)
  players:
    - name: playername         # Summoner name
      tag: euw                 # Region tag
      region: euw              # Region code
      real_name: DisplayName   # Human-readable name
  ```
- **Validation**: Checked at startup via `Config.IsValid()`

### Data Files (CRITICAL: Never Modify Directly)

**`pidors.json`** - Game state for "pidor of the day"
- **Location**: Same directory as executable
- **Format**: JSON with nested maps
- **Structure**:
  ```json
  {
    "Called": {"channelID": "2024-01-01T00:00:00Z"},
    "Stats": {"channelID": {"userID": {"name": "User", "count": 5}}}
  }
  ```
- **Access**: Only via `bot/repository/pidor.go` methods
- **WARNING**: Direct modification breaks game mechanics

**`log.json`** - Audit trail of all commands
- **Location**: Same directory as executable
- **Format**: Line-delimited JSON (one event per line)
- **Structure**: `{"timestamp": "...", "action": "...", "user": "..."}`
- **Access**: Only via `bot/repository/log.go` methods
- **WARNING**: Manual edits compromise audit trail

### Environment Variables (Required)

```bash
export BOT_DISCORD_TOKEN="your-discord-bot-token"
export BOT_OWNER_ID="discord-username-string"  # NOT user ID number
```

**CRITICAL**: 
- No validation at startup - empty strings cause silent failures
- `BOT_OWNER_ID` is username string format, not numeric user ID
- Never commit these to git

---

## Important Constraints and Pitfalls

### NEVER Modify Directly
- ❌ `pidors.json` - Causes game state corruption
- ❌ `log.json` - Breaks audit trail integrity
- ✅ Use `bot/repository/` layer for all data operations

### Known Fragile Areas

1. **Match cache is ephemeral**
   - Location: `watcher/repository/match.go`
   - In-memory only - lost on restart
   - Restart causes all recent matches to be re-notified to Discord
   - **Do NOT attempt to persist** without full database migration

2. **Mobalytics API typo**
   - File: `watcher/mobalytics/client.go:136`
   - Field: `StaredAt` should be `StartedAt`
   - **Works by accident** - JSON decoder is case-insensitive
   - **Do NOT "fix"** - will break if made case-sensitive

3. **No Discord rate limit handling**
   - File: `bot/track.go:133-148` (Fanout method)
   - Sequential channel notifications could hit Discord API limits
   - **Do NOT add high-frequency notifications** without implementing queue

4. **File handle per write**
   - Files: `bot/repository/*.go`
   - Opens file for every write operation
   - **Do NOT add high-frequency logging** without batching

5. **Pidor day logic uses system clock**
   - File: `bot/pidor.go:82-92`
   - Uses `time.Now().YearDay()` for daily reset
   - **Clock changes cause inconsistent game state**
   - **Do NOT modify** timestamp logic

6. **No environment variable validation**
   - File: `cmd/leaguewatcher/main.go`
   - Fetches env vars with `os.Getenv()` but never validates
   - Empty strings cause silent failures
   - **Add validation** if adding new env vars

### Architectural Limitations

| Limitation | Impact | Workaround |
|------------|--------|------------|
| Single process only | No horizontal scaling | Deploy multiple instances to different Discord servers |
| JSON file persistence | No query capability, no backups | Future: migrate to SQLite/PostgreSQL (see ARCHITECTURE.md roadmap) |
| Match cache lost on restart | Re-notifications after restart | Acceptable for current scale |
| No database | Limited analytics | Use `tools/pidors2csv` for exports |

---

## Code Style Guidelines

### Logging (go.uber.org/zap)

```go
// Good: Structured logging
b.logger.Info("match processed", 
    zap.String("player", match.Player.RealName),
    zap.String("champion", match.Champion),
    zap.Bool("win", match.Win))

b.logger.Error("failed to send message", 
    zap.Error(err),
    zap.String("channel", channelID))

// Bad: String interpolation
b.logger.Info(fmt.Sprintf("Player %s won", player))  // ❌ Don't do this
```

### Error Handling

```go
// In functions: Wrap errors with context
if err != nil {
    return fmt.Errorf("failed to fetch matches for %s: %w", player.Name, err)
}

// In Discord handlers: Log and return (no error to caller)
if err != nil {
    b.logger.Error("failed to execute command", zap.Error(err))
    return  // Silent return
}
```

### Naming Conventions

- **Packages**: lowercase, single word (`bot`, `watcher`, `mobalytics`)
- **Types**: PascalCase (`Match`, `Bot`, `Config`, `Player`)
- **Functions**: camelCase (`checkPlayers`, `formatMessage`, `pidorOfTheDay`)
- **Constants**: PascalCase or ALL_CAPS (`QueueRankedSolo`, `mobaAPI`)

### Concurrency Patterns

```go
// Always pass context
func (w *Watcher) Run(ctx context.Context) (<-chan Match, <-chan struct{}) {
    // Use errgroup for concurrent operations
    g, ctx := errgroup.WithContext(ctx)
    for _, player := range players {
        player := player  // Capture
        g.Go(func() error {
            return w.checkPlayer(ctx, player)
        })
    }
    
    // Close channels in defer
    defer close(matchCh)
    
    // Check context cancellation
    select {
    case <-ctx.Done():
        return
    case <-ticker.C:
        // work
    }
}
```

---

## Commit Workflow

**CRITICAL**: Follow the 3-phase workflow. Never bundle phases.

### Phase A: Style Changes
```bash
go fmt ./...
go vet ./...
git add .
git commit -m "style: apply formatting and fix linting"
go test ./...  # MUST pass
git push origin feature-branch
```

### Phase B: Refactoring
```bash
# Make structural improvements (no behavior change)
git add .
git commit -m "refactor: extract match formatting to separate function"
go test ./...  # MUST pass
git push origin feature-branch
```

### Phase C: Business Logic
```bash
# Implement actual feature/fix
git add .
git commit -m "feat: add champion mastery display"
go test ./...  # MUST pass
git push origin feature-branch
```

### Commit Message Format
```
<type>: <description>

[optional body]
```

**Types**: `feat`, `fix`, `refactor`, `style`, `test`, `docs`, `chore`

**Examples**:
- `feat: add !stats command for match statistics`
- `fix: correct LP calculation for placement matches`
- `refactor: extract emoji selection to helper function`
- `style: apply gofmt to all files`

---

## External Dependencies

### Discord (`github.com/bwmarrin/discordgo` v0.28.1)
- **Protocol**: WebSocket gateway (events) + REST API (messages)
- **Authentication**: Bot token via `BOT_DISCORD_TOKEN`
- **Required permissions**: Read Messages, Send Messages, Manage Members
- **Commands**: Prefix-based only (`!command`) - no slash commands
- **Rate limits**: Handled by library (no custom implementation)

### Mobalytics (GraphQL over HTTPS)
- **Endpoint**: `https://mobalytics.gg/api/lol/graphql/v1/query`
- **Authentication**: None (public API)
- **Timeout**: 30 seconds per request (hardcoded in client)
- **Region codes**: Uppercased for API (`EUW`), lowercased in storage (`euw`)
- **Stability**: Third-party service, no SLA

### Go Dependencies
- `github.com/hasura/go-graphql-client` - GraphQL client
- `github.com/kyokomi/emoji/v2` - Emoji rendering for Discord
- `go.uber.org/zap` - Structured logging
- `golang.org/x/sync/errgroup` - Concurrent error handling
- `gopkg.in/yaml.v3` - Config file parsing

---

## Security Considerations

### Secrets Management
- ❌ NEVER commit `BOT_DISCORD_TOKEN` to git
- ✅ Use environment variables or secure vault
- ✅ `.gitignore` includes `.env` files

### Authorization
- Owner-only commands: `m.Author.String() == b.cfg.OwnerID`
- Authorization is **username string comparison**, not numeric user ID
- Example: `BOT_OWNER_ID="cauchy2384"` (not `BOT_OWNER_ID="123456789"`)

### Data Exposure
- Config logging sanitizes sensitive fields
- `pidors.json` and `log.json` contain no secrets
- Discord tokens never logged

---

## Testing Guidelines

### Test Data Location
- **Path**: `internal/leaguewatcher/testdata/config.yaml`
- **Purpose**: Minimal valid configuration for tests
- **Used by**: `config_test.go` for YAML parsing validation

### Integration Tests
- **Location**: `watcher/mobalytics/client_test.go`
- **Behavior**: Makes real HTTP calls to Mobalytics API
- **Requirements**: Internet connectivity, real summoner names
- **Execution**: Slower than unit tests, may fail if API is down

### Test Requirements
- All tests must pass before PR submission
- Use `t.Parallel()` for independent tests
- Prefer table-driven test pattern
- No TODO/FIXME comments in codebase (clean state)

---

## Quick Reference

| Task | Command/Location |
|------|------------------|
| **Run bot** | `go run cmd/leaguewatcher/main.go` |
| **Build binary** | `go build -o leaguewatcherbot cmd/leaguewatcher/main.go` |
| **Run all tests** | `go test ./...` |
| **Format code** | `go fmt ./...` |
| **Lint code** | `go vet ./...` |
| **Add Discord command** | Create in `bot/`, register in `bot.go:cmd()` |
| **Add API query** | Add to `mobalytics/client.go` |
| **Test pattern** | Table-driven with `t.Run()` + `t.Parallel()` |
| **Config file** | `config.yaml` in executable directory |
| **Data files** | `pidors.json`, `log.json` (auto-generated) |
| **Env vars** | `BOT_DISCORD_TOKEN`, `BOT_OWNER_ID` |
| **Assertion library** | `github.com/matryer/is` (preferred) |
| **Logging** | `go.uber.org/zap` structured logging |

---

## Additional Resources

- **Architecture**: [ARCHITECTURE.md](ARCHITECTURE.md) - System design and components
- **Contributing**: [CONTRIBUTING.md](CONTRIBUTING.md) - Human contributor guide
- **Repository**: [github.com/cauchy2384/leaguewatcherbot](https://github.com/cauchy2384/leaguewatcherbot)
- **License**: [MIT License](LICENSE)
- **Go Documentation**: [pkg.go.dev](https://pkg.go.dev/)
- **Discord API**: [discord.com/developers/docs](https://discord.com/developers/docs)

---

## Architecture Decision Records (ADRs)

**Location**: `adr/` directory in repository root

### When to Create ADRs

**CRITICAL**: Create an ADR for:
- New features or significant functionality changes
- Architectural decisions (technology choices, design patterns)
- Breaking changes or major refactors
- Removal or disabling of existing features
- Infrastructure or deployment changes

**Format**: Follow existing ADR structure (see `adr/001-docker-containerization.md` as reference):
- **Status**: Accepted, Proposed, Deprecated, Superseded
- **Date**: YYYY-MM-DD
- **Context**: Why is this decision needed? What problem are we solving?
- **Decision**: What are we doing? Be specific about the approach.
- **Consequences**: 
  - **Positive**: What benefits does this bring?
  - **Negative**: What drawbacks or risks?
  - **Neutral**: What are the trade-offs or things to note?
- **Alternatives Considered**: What other approaches were evaluated and why were they rejected?
- **Future Work**: What needs to happen next? What's the plan for follow-up?
- **References**: Code locations, documentation, external resources
- **Reviewers**: Author, approver, date
- **Changelog**: History of updates to this ADR

**Numbering**: Use next sequential number (001, 002, 003, etc.)

### Examples

- **adr/001-docker-containerization.md** - Docker deployment decision, multi-stage builds, volume strategy
- **adr/002-ci-cd-docker-pipeline.md** - CI/CD pipeline with semantic versioning, automated releases
- **adr/003-disable-profile-refresh.md** - Temporarily disabling broken Mobalytics profile refresh feature

### ADR Best Practices

1. **Write ADRs during implementation**, not after - capture context while fresh
2. **Be specific**: Include file paths, line numbers, configuration values
3. **Document alternatives**: Future developers need to understand why you chose this path
4. **Update ADRs**: If decision is superseded or reversed, update the status and add changelog entry
5. **Reference ADRs in code**: Use `// See ADR-XXX` comments to link decisions to implementation
6. **Link related ADRs**: Cross-reference when decisions build on or conflict with previous ones

---

**Last Updated**: 2026-05-04  
**Go Version**: 1.22.0+  
**Maintainer**: [@cauchy2384](https://github.com/cauchy2384)
