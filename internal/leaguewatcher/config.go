package leaguewatcher

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/nikoksr/doppler-go"
	"github.com/nikoksr/doppler-go/secret"
	"gopkg.in/yaml.v3"
)

type Config struct {
	PollPeriod time.Duration `yaml:"poll_period"`
	PlayedGap  time.Duration `yaml:"played_gap"`

	Players           []Player `yaml:"players"`
	ChannelID         string   `yaml:"channel_id"`
	KhaleesiThreshold *int     `yaml:"khaleesi_threshold,omitempty"`

	// Secrets from Doppler (never logged)
	DiscordToken string `yaml:"-"` // BOT_DISCORD_TOKEN
	OwnerID      string `yaml:"-"` // BOT_OWNER_ID

	// Nebius Token Factory configuration (from Doppler)
	TofaAPIKey          string  `yaml:"-"` // TOFA_API_KEY (secret, never logged)
	TofaModel           string  `yaml:"-"` // TOFA_MODEL
	TofaBaseURL         string  `yaml:"-"` // TOFA_BASE_URL
	TofaSystemPrompt    string  `yaml:"-"` // TOFA_SYSTEM_PROMPT
	TofaTemperature     float32 `yaml:"-"` // TOFA_TEMPERATURE
	TofaTopP            float32 `yaml:"-"` // TOFA_TOP_P
	TofaMaxOutputTokens int32   `yaml:"-"` // TOFA_MAX_OUTPUT_TOKENS

	// Tavily Search configuration (optional, from Doppler). Used to ground
	// LLM answers with web search results. Missing API key silently
	// disables the feature.
	TavilyAPIKey         string `yaml:"-"` // TAVILY_API_KEY (secret, never logged)
	TavilyBaseURL        string `yaml:"-"` // TAVILY_BASE_URL
	TavilySearchDepth    string `yaml:"-"` // TAVILY_SEARCH_DEPTH
	TavilyTopic          string `yaml:"-"` // TAVILY_TOPIC
	TavilyMaxResults     int    `yaml:"-"` // TAVILY_MAX_RESULTS
	TavilyTimeoutSeconds int    `yaml:"-"` // TAVILY_TIMEOUT_SECONDS
	TavilyIncludeDomains string `yaml:"-"` // TAVILY_INCLUDE_DOMAINS (CSV)
	TavilyExcludeDomains string `yaml:"-"` // TAVILY_EXCLUDE_DOMAINS (CSV)

	// Outer ask budget. Covers the optional Tavily search step and the
	// LLM call together. Default 15s matches the historical hardcoded value.
	AskTimeoutSeconds int `yaml:"-"` // ASK_TIMEOUT_SECONDS

	// Neon PostgreSQL (from Doppler)
	DatabaseURL       string `yaml:"-"` // DATABASE_URL (direct endpoint, migrations/tools)
	DatabaseURLPooled string `yaml:"-"` // DATABASE_URL_POOLED (pooled endpoint, runtime traffic)
	NeonAuthBaseURL   string `yaml:"-"` // NEON_AUTH_BASE_URL
}

func (cfg Config) IsValid() error {
	if cfg.PollPeriod <= 0 {
		return fmt.Errorf("poll_period must be positive")
	}
	if cfg.PlayedGap <= 0 {
		return fmt.Errorf("played_gap must be positive")
	}

	if len(cfg.Players) == 0 {
		return fmt.Errorf("players must not be empty")
	}

	for i, p := range cfg.Players {
		if p.Name == "" {
			return fmt.Errorf("player %d name must not be empty", i)
		}
		if p.Region == "" {
			return fmt.Errorf("player %d region must not be empty", i)
		}
		if p.RealName == "" {
			return fmt.Errorf("player %d real name must not be empty", i)
		}
	}

	if cfg.ChannelID == "" {
		return fmt.Errorf("channel ID must not be empty")
	}

	if cfg.KhaleesiThreshold != nil && *cfg.KhaleesiThreshold < 0 {
		return fmt.Errorf("khaleesi_threshold must be >= 0")
	}

	if cfg.DiscordToken == "" {
		return fmt.Errorf("discord token must not be empty")
	}

	if cfg.OwnerID == "" {
		return fmt.Errorf("owner ID must not be empty")
	}

	return nil
}

// LogValue implements slog.LogValuer to prevent secrets from being logged
func (cfg Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Duration("poll_period", cfg.PollPeriod),
		slog.Duration("played_gap", cfg.PlayedGap),
		slog.Int("num_players", len(cfg.Players)),
		slog.String("channel_id", cfg.ChannelID),
		slog.Any("khaleesi_threshold", cfg.KhaleesiThreshold),
		slog.String("discord_token", "***REDACTED***"),
		slog.String("owner_id", "***REDACTED***"),
		slog.String("tofa_api_key", "***REDACTED***"),
		slog.String("tofa_model", cfg.TofaModel),
		slog.String("tofa_base_url", cfg.TofaBaseURL),
		slog.Float64("tofa_temperature", float64(cfg.TofaTemperature)),
		slog.Float64("tofa_top_p", float64(cfg.TofaTopP)),
		slog.Int("tofa_max_output_tokens", int(cfg.TofaMaxOutputTokens)),
		slog.String("tavily_api_key", "***REDACTED***"),
		slog.String("tavily_base_url", cfg.TavilyBaseURL),
		slog.String("tavily_search_depth", cfg.TavilySearchDepth),
		slog.String("tavily_topic", cfg.TavilyTopic),
		slog.Int("tavily_max_results", cfg.TavilyMaxResults),
		slog.Int("tavily_timeout_seconds", cfg.TavilyTimeoutSeconds),
		slog.String("tavily_include_domains", cfg.TavilyIncludeDomains),
		slog.String("tavily_exclude_domains", cfg.TavilyExcludeDomains),
		slog.Int("ask_timeout_seconds", cfg.AskTimeoutSeconds),
		slog.Bool("database_configured", cfg.DatabaseURL != "" || cfg.DatabaseURLPooled != ""),
		slog.Bool("neon_auth_configured", cfg.NeonAuthBaseURL != ""),
	)
}

// SecretProvider is an interface for fetching secrets (enables testing with fakes)
type SecretProvider interface {
	ListSecrets(ctx context.Context) (map[string]*doppler.SecretValue, error)
}

// dopplerSecretProvider is the real implementation using Doppler SDK
type dopplerSecretProvider struct{}

func (d *dopplerSecretProvider) ListSecrets(ctx context.Context) (map[string]*doppler.SecretValue, error) {
	secrets, _, err := secret.List(ctx, &doppler.SecretListOptions{
		Project: "leaguewatcherbot",
		Config:  "prd",
	})
	if err != nil {
		return nil, err
	}
	return secrets, nil
}

// ConfigManager manages configuration loaded from Doppler with hot reload support
type ConfigManager struct {
	mu             sync.RWMutex
	config         Config
	logger         *slog.Logger
	token          string
	secretProvider SecretProvider
}

// NewConfigManager creates a new ConfigManager with the given Doppler token
// The token should be a Doppler Service Token which is scoped to a specific project and config
func NewConfigManager(token string, logger *slog.Logger) (*ConfigManager, error) {
	if token == "" {
		return nil, fmt.Errorf("doppler token is required")
	}

	// Set the global Doppler API key
	doppler.Key = token

	return &ConfigManager{
		token:          token,
		logger:         logger,
		secretProvider: &dopplerSecretProvider{},
	}, nil
}

// newConfigManagerWithProvider creates a ConfigManager with a custom SecretProvider (for testing)
func newConfigManagerWithProvider(token string, logger *slog.Logger, provider SecretProvider) *ConfigManager {
	return &ConfigManager{
		token:          token,
		logger:         logger,
		secretProvider: provider,
	}
}

// Reload fetches the latest configuration from Doppler and updates the internal config
func (cm *ConfigManager) Reload(ctx context.Context) error {
	cm.logger.Info("reloading configuration from Doppler")

	// Use the secret provider (real or fake for testing)
	secrets, err := cm.secretProvider.ListSecrets(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch secrets from Doppler: %w", err)
	}

	// Parse configuration from secrets
	var newConfig Config

	// Parse poll_period
	if pollPeriodSecret, ok := secrets["POLL_PERIOD"]; ok && pollPeriodSecret.Computed != nil {
		pollPeriod, err := time.ParseDuration(*pollPeriodSecret.Computed)
		if err != nil {
			return fmt.Errorf("failed to parse poll_period: %w", err)
		}
		newConfig.PollPeriod = pollPeriod
	}

	// Parse played_gap
	if playedGapSecret, ok := secrets["PLAYED_GAP"]; ok && playedGapSecret.Computed != nil {
		playedGap, err := time.ParseDuration(*playedGapSecret.Computed)
		if err != nil {
			return fmt.Errorf("failed to parse played_gap: %w", err)
		}
		newConfig.PlayedGap = playedGap
	}

	// Parse channel_id
	if channelIDSecret, ok := secrets["CHANNEL_ID"]; ok && channelIDSecret.Computed != nil {
		newConfig.ChannelID = *channelIDSecret.Computed
	}

	// Parse khaleesi_threshold (optional)
	if khaleesiThresholdSecret, ok := secrets["KHALEESI_THRESHOLD"]; ok && khaleesiThresholdSecret.Computed != nil {
		threshold, err := strconv.Atoi(*khaleesiThresholdSecret.Computed)
		if err != nil {
			return fmt.Errorf("failed to parse khaleesi_threshold: %w", err)
		}
		newConfig.KhaleesiThreshold = &threshold
	}

	// Parse players from YAML
	if playersYAMLSecret, ok := secrets["PLAYERS_YAML"]; ok && playersYAMLSecret.Computed != nil {
		var players []Player
		if err := yaml.Unmarshal([]byte(*playersYAMLSecret.Computed), &players); err != nil {
			return fmt.Errorf("failed to parse PLAYERS_YAML: %w", err)
		}
		newConfig.Players = players
	}

	// Parse discord_token (required)
	if discordTokenSecret, ok := secrets["BOT_DISCORD_TOKEN"]; ok && discordTokenSecret.Computed != nil {
		newConfig.DiscordToken = *discordTokenSecret.Computed
	}

	// Parse owner_id (required)
	if ownerIDSecret, ok := secrets["BOT_OWNER_ID"]; ok && ownerIDSecret.Computed != nil {
		newConfig.OwnerID = *ownerIDSecret.Computed
	}

	// Parse Nebius Token Factory configuration
	if tofaAPIKeySecret, ok := secrets["TOFA_API_KEY"]; ok && tofaAPIKeySecret.Computed != nil {
		newConfig.TofaAPIKey = *tofaAPIKeySecret.Computed
	}

	if tofaModelSecret, ok := secrets["TOFA_MODEL"]; ok && tofaModelSecret.Computed != nil {
		newConfig.TofaModel = *tofaModelSecret.Computed
	}
	if newConfig.TofaModel == "" {
		newConfig.TofaModel = "meta-llama/Llama-3.3-70B-Instruct"
	}

	if tofaBaseURLSecret, ok := secrets["TOFA_BASE_URL"]; ok && tofaBaseURLSecret.Computed != nil {
		newConfig.TofaBaseURL = *tofaBaseURLSecret.Computed
	}
	if newConfig.TofaBaseURL == "" {
		newConfig.TofaBaseURL = "https://api.tokenfactory.nebius.com/v1"
	}

	if tofaSystemPromptSecret, ok := secrets["TOFA_SYSTEM_PROMPT"]; ok && tofaSystemPromptSecret.Computed != nil {
		newConfig.TofaSystemPrompt = *tofaSystemPromptSecret.Computed
	}

	// Parse Neon PostgreSQL configuration (optional - enables DB-backed repositories)
	if databaseURLSecret, ok := secrets["DATABASE_URL"]; ok && databaseURLSecret.Computed != nil {
		newConfig.DatabaseURL = *databaseURLSecret.Computed
	}

	if databaseURLPooledSecret, ok := secrets["DATABASE_URL_POOLED"]; ok && databaseURLPooledSecret.Computed != nil {
		newConfig.DatabaseURLPooled = *databaseURLPooledSecret.Computed
	}

	if neonAuthBaseURLSecret, ok := secrets["NEON_AUTH_BASE_URL"]; ok && neonAuthBaseURLSecret.Computed != nil {
		newConfig.NeonAuthBaseURL = *neonAuthBaseURLSecret.Computed
	}

	// Parse TOFA generation parameters with defaults
	newConfig.TofaTemperature = 0.4
	if tempSecret, ok := secrets["TOFA_TEMPERATURE"]; ok && tempSecret.Computed != nil {
		temp, err := strconv.ParseFloat(*tempSecret.Computed, 32)
		if err == nil {
			newConfig.TofaTemperature = float32(temp)
		}
	}

	newConfig.TofaTopP = 0.95
	if topPSecret, ok := secrets["TOFA_TOP_P"]; ok && topPSecret.Computed != nil {
		tp, err := strconv.ParseFloat(*topPSecret.Computed, 32)
		if err == nil {
			newConfig.TofaTopP = float32(tp)
		}
	}

	newConfig.TofaMaxOutputTokens = 2048
	if maxTokensSecret, ok := secrets["TOFA_MAX_OUTPUT_TOKENS"]; ok && maxTokensSecret.Computed != nil {
		mt, err := strconv.Atoi(*maxTokensSecret.Computed)
		if err == nil {
			newConfig.TofaMaxOutputTokens = int32(mt)
		}
	}

	// Parse Tavily Search configuration (optional). All fields have
	// reasonable defaults; only TAVILY_API_KEY activates the feature.
	if tavilyAPIKeySecret, ok := secrets["TAVILY_API_KEY"]; ok && tavilyAPIKeySecret.Computed != nil {
		newConfig.TavilyAPIKey = *tavilyAPIKeySecret.Computed
	}

	if tavilyBaseURLSecret, ok := secrets["TAVILY_BASE_URL"]; ok && tavilyBaseURLSecret.Computed != nil {
		newConfig.TavilyBaseURL = *tavilyBaseURLSecret.Computed
	}
	if newConfig.TavilyBaseURL == "" {
		newConfig.TavilyBaseURL = "https://api.tavily.com"
	}

	if tavilySearchDepthSecret, ok := secrets["TAVILY_SEARCH_DEPTH"]; ok && tavilySearchDepthSecret.Computed != nil {
		newConfig.TavilySearchDepth = *tavilySearchDepthSecret.Computed
	}
	if newConfig.TavilySearchDepth == "" {
		newConfig.TavilySearchDepth = "basic"
	}

	if tavilyTopicSecret, ok := secrets["TAVILY_TOPIC"]; ok && tavilyTopicSecret.Computed != nil {
		newConfig.TavilyTopic = *tavilyTopicSecret.Computed
	}
	if newConfig.TavilyTopic == "" {
		newConfig.TavilyTopic = "general"
	}

	newConfig.TavilyMaxResults = 3
	if tavilyMaxResultsSecret, ok := secrets["TAVILY_MAX_RESULTS"]; ok && tavilyMaxResultsSecret.Computed != nil {
		mr, err := strconv.Atoi(*tavilyMaxResultsSecret.Computed)
		if err == nil {
			newConfig.TavilyMaxResults = mr
		}
	}

	newConfig.TavilyTimeoutSeconds = 5
	if tavilyTimeoutSecret, ok := secrets["TAVILY_TIMEOUT_SECONDS"]; ok && tavilyTimeoutSecret.Computed != nil {
		ts, err := strconv.Atoi(*tavilyTimeoutSecret.Computed)
		if err == nil {
			newConfig.TavilyTimeoutSeconds = ts
		}
	}

	if tavilyIncludeSecret, ok := secrets["TAVILY_INCLUDE_DOMAINS"]; ok && tavilyIncludeSecret.Computed != nil {
		newConfig.TavilyIncludeDomains = *tavilyIncludeSecret.Computed
	}
	if tavilyExcludeSecret, ok := secrets["TAVILY_EXCLUDE_DOMAINS"]; ok && tavilyExcludeSecret.Computed != nil {
		newConfig.TavilyExcludeDomains = *tavilyExcludeSecret.Computed
	}

	// Parse outer ask budget (covers search + LLM together).
	newConfig.AskTimeoutSeconds = 15
	if askTimeoutSecret, ok := secrets["ASK_TIMEOUT_SECONDS"]; ok && askTimeoutSecret.Computed != nil {
		at, err := strconv.Atoi(*askTimeoutSecret.Computed)
		if err == nil && at > 0 {
			newConfig.AskTimeoutSeconds = at
		}
	}

	// Validate the new config
	if err := newConfig.IsValid(); err != nil {
		return fmt.Errorf("invalid configuration from Doppler: %w", err)
	}

	// Update the config atomically
	cm.mu.Lock()
	cm.config = newConfig
	cm.mu.Unlock()

	cm.logger.Info("configuration reloaded successfully", slog.Any("config", newConfig))
	return nil
}

// Get returns the current configuration (thread-safe read)
func (cm *ConfigManager) Get() Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config
}

// GetPlayers returns the current player list (thread-safe read)
func (cm *ConfigManager) GetPlayers() []Player {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.Players
}

// StartAutoReload starts a background goroutine that periodically reloads configuration from Doppler
// Returns a channel that closes when the goroutine exits (for graceful shutdown)
func (cm *ConfigManager) StartAutoReload(ctx context.Context, interval time.Duration) <-chan struct{} {
	done := make(chan struct{})
	ticker := time.NewTicker(interval)

	go func() {
		defer close(done)
		defer ticker.Stop()
		defer cm.logger.Info("auto-reload stopped")

		for {
			select {
			case <-ticker.C:
				if err := cm.Reload(ctx); err != nil {
					cm.logger.Error("failed to auto-reload configuration", "error", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return done
}
