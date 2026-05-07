# ADR 004: Doppler SDK Integration for Configuration Management

## Status

**Accepted** - 2026-05-07

## Context

The League Watcher Bot currently uses a local `config.yaml` file for configuration management with the following limitations:

1. **No Configuration History**: Changes to config are not tracked or versioned
2. **No Rollback Capability**: Reverting to previous configurations requires manual backups
3. **Static Configuration**: Changes require container restart, causing downtime
4. **File-based Security**: Secrets and config stored on disk alongside the binary
5. **No Hot Reload**: Adding/removing players requires full bot restart
6. **Manual Sync**: Config changes must be manually propagated to deployment environments

### Problems with Current Approach

- **Operational Friction**: Every config change requires rebuild + redeploy
- **Downtime**: Restarting bot for config changes interrupts match monitoring
- **No Audit Trail**: Can't answer "who changed what when" for config
- **Limited Flexibility**: Can't dynamically adjust player list during events
- **Security Concerns**: Config file contains channel IDs and other settings in plaintext
- **Deployment Complexity**: Each environment needs its own config.yaml

### Requirements for Solution

1. **Managed Configuration**: Centralized config management with versioning and audit trail
2. **Hot Reload**: Apply config changes without restarting the bot
3. **Security**: Secrets managed separately from code, fetched at runtime
4. **Zero-Downtime Updates**: Player list changes take effect within minutes, not hours
5. **Simple Infrastructure**: No complex sidecar containers or CLI installations
6. **Backward Compatibility**: Maintain existing config structure for gradual migration

## Decision

We will **integrate the Doppler Go SDK** to fetch configuration directly from Doppler's API using Service Tokens:

### 1. ConfigManager Implementation

**Location**: `internal/leaguewatcher/config.go`

**Core Components**:
```go
type ConfigManager struct {
    mu     sync.RWMutex
    config Config
    logger *slog.Logger
    token  string
}
```

**Key Methods**:
- `NewConfigManager(token, logger)` - Initialize with Doppler Service Token
- `Reload(ctx)` - Fetch latest config from Doppler API
- `Get()` - Thread-safe read of current config
- `GetPlayers()` - Thread-safe read of player list
- `StartAutoReload(ctx, interval)` - Background goroutine for periodic refreshes (default: 5 minutes)

**Implementation Details**:
- Uses `github.com/nikoksr/doppler-go` SDK
- Service Token scoped to specific project/config (no project/config parameters needed)
- `RWMutex` for thread-safe config updates
- Validates config before applying (uses existing `Config.IsValid()`)
- Logs all reload attempts and failures

### 2. Doppler Secret Mapping

**Doppler Secret** → **Go Config Field**

| Doppler Secret | Type | Config Field | Example |
|----------------|------|--------------|---------|
| `POLL_PERIOD` | Duration | `PollPeriod` | `1m` |
| `PLAYED_GAP` | Duration | `PlayedGap` | `60m` |
| `CHANNEL_ID` | String | `ChannelID` | `123456789` |
| `KHALEESI_THRESHOLD` | Int (optional) | `KhaleesiThreshold` | `5` |
| `PLAYERS_YAML` | YAML String | `Players []Player` | See below |

**PLAYERS_YAML Format**:
```yaml
- name: playername
  tag: euw
  region: euw
  real_name: DisplayName
- name: anotherplayer
  tag: na
  region: na
  real_name: PlayerTwo
```

### 3. Dynamic Updates in Components

**Watcher** (`internal/leaguewatcher/watcher/watcher.go`):
- Accepts `ConfigProvider` interface instead of static config
- `checkPlayers()` fetches latest player list on each iteration
- Supports adding/removing players without restart
- Period and PlayedGap remain static (changes require restart)

**ConfigProvider Interface**:
```go
type ConfigProvider interface {
    GetPlayers() []Player
    Get() Config
}
```

**Bot**:
- Currently receives static config at initialization
- ChannelID and KhaleesiThreshold are static (changes require restart)
- Future enhancement: Support dynamic threshold updates

### 4. Main Entry Point Refactor

**Changes in `cmd/leaguewatcher/main.go`**:
1. Fetch `DOPPLER_TOKEN` from environment (required, fail fast if missing)
2. Initialize `ConfigManager` with token
3. Perform initial `Reload()` to fetch config
4. Start `StartAutoReload(ctx, 5*time.Minute)` for background refreshes
5. Pass `ConfigManager` to watcher (implements `ConfigProvider`)
6. Pass static `Config` snapshot to bot

**Error Handling**:
- Missing `DOPPLER_TOKEN`: Fail at startup with clear error
- Initial reload failure: Fail at startup (no default config)
- Auto-reload failures: Log error but continue with last known good config

### 5. Infrastructure Changes

**Dockerfile**:
- ❌ Remove: `COPY config.yaml /app/config.yaml`
- Config now fetched from Doppler API, not bundled in image

**docker-compose.yml**:
- ❌ Remove: `./config.yaml:/app/config.yaml:ro` volume mount
- ✅ Add: `DOPPLER_TOKEN=${DOPPLER_TOKEN}` environment variable

**Required Setup**:
1. Create Doppler project and config (e.g., `leaguewatcher` / `prd`)
2. Add all config secrets to Doppler dashboard
3. Generate Service Token for the config
4. Add `DOPPLER_TOKEN=dp.st.xxx` to `.env` file

## Consequences

### Positive

1. **Managed Configuration**: Full version history and rollback via Doppler dashboard
2. **Hot Reload**: Player list changes take effect within 5 minutes (configurable interval)
3. **Security**: Secrets fetched from Doppler API, never on disk
4. **Audit Trail**: Doppler tracks who changed what and when
5. **Zero-Downtime Updates**: No container restart needed for player list changes
6. **Clean Infrastructure**: No config.yaml in image or volume mounts
7. **Environment Parity**: Same config structure across dev/staging/prod, different Doppler configs
8. **Simplified Deployment**: Single `DOPPLER_TOKEN` env var instead of managing config files

### Negative

1. **External Dependency**: Doppler API outage prevents bot startup (but existing instances continue with cached config)
2. **Network Requirement**: Periodic API calls to Doppler (minimal overhead, ~1 request/5min)
3. **Cost**: Doppler Free tier supports 5 users; may require paid plan for team growth
4. **Complexity**: Additional component to manage vs simple YAML file
5. **Learning Curve**: Team must learn Doppler dashboard for config changes

### Neutral

1. **Partial Hot Reload**: Player list updates dynamically, but Period/PlayedGap/ChannelID still require restart
   - **Why**: These are fundamental runtime parameters set at component initialization
   - **Future**: Could be extended to support full hot reload if needed
2. **Service Token Management**: Need to rotate tokens periodically for security
   - **Mitigation**: Doppler supports token rotation without downtime
3. **Config Format Change**: PLAYERS_YAML is YAML string in Doppler, not JSON
   - **Benefit**: More readable in Doppler dashboard than JSON array
   - **Trade-off**: Slightly more complex parsing (yaml.Unmarshal on string value)

## Alternatives Considered

### Alternative 1: Doppler CLI + Sidecar Pattern

**Approach**: Run Doppler CLI in sidecar container, write secrets to shared volume, app reads from file

**Rejected Because**:
- More complex infrastructure (sidecar container, shared volumes)
- CLI installation increases image size
- Still relies on file I/O, negating API benefits
- No native hot reload (requires file watcher)
- Harder to debug (two processes instead of one)

### Alternative 2: Environment Variables Only

**Approach**: Pass all config as individual environment variables (no Doppler SDK)

**Rejected Because**:
- No hot reload (env vars are static at container start)
- Player list as env var is unwieldy (JSON string or 20+ individual vars)
- No built-in versioning or audit trail
- Still need config management tool (e.g., Doppler) for secret storage
- Loses benefit of centralized config UI

### Alternative 3: Embed Doppler CLI in Docker Image

**Approach**: Install Doppler CLI in Docker image, run `doppler run` wrapper

**Rejected Because**:
- Increases image size (~40MB → ~60MB)
- Adds external binary dependency
- Doesn't support hot reload natively
- SDK approach is more idiomatic for Go apps
- CLI designed for shell environments, not long-running services

### Alternative 4: Config File + File Watcher

**Approach**: Keep config.yaml, add file watcher for hot reload

**Rejected Because**:
- No version control or audit trail
- Manual config distribution across environments
- Still need external config management for secrets
- File watchers add complexity and edge cases (partial writes, race conditions)
- Doesn't solve the "config as code" problem

### Alternative 5: Database-backed Configuration

**Approach**: Store config in PostgreSQL/SQLite, poll for changes

**Rejected Because**:
- Massive over-engineering for simple config needs
- Introduces database dependency and operational complexity
- No built-in UI for config management
- Requires custom migration and versioning logic
- Overkill for ~10 config values

## Future Work

1. **Full Hot Reload Support** (Optional):
   - Extend dynamic updates to `ChannelID`, `KhaleesiThreshold`, `PollPeriod`, `PlayedGap`
   - Requires refactoring bot and watcher to support runtime reconfiguration
   - **Effort**: Medium (2-3 days)
   - **Value**: Low (these rarely change in production)

2. **Doppler Webhooks** (Optional):
   - Subscribe to Doppler webhooks for instant config updates instead of polling
   - Reduces reload latency from 5 minutes to <1 second
   - **Effort**: Low (1 day)
   - **Value**: Low (5-minute latency is acceptable for current use case)

3. **Config Validation UI** (Optional):
   - Custom Doppler app or script to validate PLAYERS_YAML format before save
   - Prevents invalid YAML from breaking bot
   - **Effort**: Medium (2 days)
   - **Value**: Medium (nice-to-have safety net)

4. **Multi-Environment Setup** (Recommended):
   - Create separate Doppler configs for `dev`, `staging`, `prd`
   - Document token generation and rotation procedures
   - **Effort**: Low (half day)
   - **Value**: High (production best practice)

5. **Fallback to Local Config** (Optional):
   - Support reading from `config.yaml` if `DOPPLER_TOKEN` is not set
   - Useful for local development without Doppler account
   - **Effort**: Low (1 day)
   - **Value**: Medium (convenience for contributors)

## References

### Code Locations

- **ConfigManager**: `internal/leaguewatcher/config.go:53-156`
- **Watcher Integration**: `internal/leaguewatcher/watcher/watcher.go:14-27, 80-84`
- **Main Entry Point**: `cmd/leaguewatcher/main.go:35-52`
- **Dockerfile**: `Dockerfile:43-48` (config.yaml removed)
- **Docker Compose**: `docker-compose.yml:17-26` (DOPPLER_TOKEN added)

### External Documentation

- **Doppler Go SDK**: https://github.com/nikoksr/doppler-go
- **Doppler Service Tokens**: https://docs.doppler.com/docs/service-tokens
- **Doppler API Reference**: https://docs.doppler.com/reference/api

### Related ADRs

- **ADR-001**: Docker Containerization (infrastructure foundation)
- **ADR-002**: CI/CD Pipeline (deployment automation)

## Reviewers

- **Author**: Claude Sonnet 4.5 (AI Assistant)
- **Date**: 2026-05-07

## Changelog

- **2026-05-07**: Initial version - Doppler SDK integration decision
