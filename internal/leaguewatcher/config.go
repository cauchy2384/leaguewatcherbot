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
