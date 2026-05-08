package leaguewatcher

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/nikoksr/doppler-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConfig(t *testing.T) {
	t.Parallel()

	fd, err := os.Open(filepath.Join("testdata", "config.yaml"))
	require.NoError(t, err)
	t.Cleanup(func() { fd.Close() })

	var cfg Config
	err = yaml.NewDecoder(fd).Decode(&cfg)
	require.NoError(t, err)
	t.Log(cfg)

	assert.NotEmpty(t, cfg.PollPeriod)
	assert.NotEmpty(t, cfg.PlayedGap)

	assert.NotNil(t, cfg.Players)
	for _, p := range cfg.Players {
		assert.NotEmpty(t, p.Name)
		assert.NotEmpty(t, p.Tag)
		assert.NotEmpty(t, p.Region)
		assert.NotEmpty(t, p.RealName)
	}

	// Set secrets that are normally from Doppler (not in YAML)
	cfg.DiscordToken = "test-token"
	cfg.OwnerID = "test-owner"

	assert.NoError(t, cfg.IsValid())
}

// fakeSecretProvider is a fake implementation of SecretProvider for testing
type fakeSecretProvider struct {
	secrets map[string]*doppler.SecretValue
	err     error
}

func (f *fakeSecretProvider) ListSecrets(ctx context.Context) (map[string]*doppler.SecretValue, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.secrets, nil
}

// newFakeSecretProvider creates a fake with the given secrets
func newFakeSecretProvider(secrets map[string]*doppler.SecretValue) *fakeSecretProvider {
	return &fakeSecretProvider{
		secrets: secrets,
		err:     nil,
	}
}

// newFakeSecretProviderWithError creates a fake that returns an error
func newFakeSecretProviderWithError(err error) *fakeSecretProvider {
	return &fakeSecretProvider{
		secrets: nil,
		err:     err,
	}
}

// stringPtr is a helper to create string pointers
func stringPtr(s string) *string {
	return &s
}

// createTestSecrets creates a complete set of test secrets for ConfigManager testing
func createTestSecrets() map[string]*doppler.SecretValue {
	return map[string]*doppler.SecretValue{
		"POLL_PERIOD": {
			Computed: stringPtr("2m"),
		},
		"PLAYED_GAP": {
			Computed: stringPtr("30m"),
		},
		"CHANNEL_ID": {
			Computed: stringPtr("123456789"),
		},
		"KHALEESI_THRESHOLD": {
			Computed: stringPtr("5"),
		},
		"BOT_DISCORD_TOKEN": {
			Computed: stringPtr("test-discord-token-12345"),
		},
		"BOT_OWNER_ID": {
			Computed: stringPtr("test-owner#1234"),
		},
		"PLAYERS_YAML": {
			Computed: stringPtr(`- name: player1
  tag: euw
  region: euw
  real_name: Player One
- name: player2
  tag: na
  region: na
  real_name: Player Two`),
		},
	}
}

func TestConfigManager_Reload_Success(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during tests
	}))

	fakeProvider := newFakeSecretProvider(createTestSecrets())
	cm := newConfigManagerWithProvider("test-token", logger, fakeProvider)

	err := cm.Reload(context.Background())
	is.NoErr(err)

	cfg := cm.Get()
	is.Equal(cfg.PollPeriod, 2*time.Minute)
	is.Equal(cfg.PlayedGap, 30*time.Minute)
	is.Equal(cfg.ChannelID, "123456789")
	is.Equal(*cfg.KhaleesiThreshold, 5)
	is.Equal(cfg.DiscordToken, "test-discord-token-12345")
	is.Equal(cfg.OwnerID, "test-owner#1234")
	is.Equal(len(cfg.Players), 2)
	is.Equal(cfg.Players[0].Name, "player1")
	is.Equal(cfg.Players[1].RealName, "Player Two")
}

func TestConfigManager_Reload_MissingRequiredSecret(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	secrets := createTestSecrets()
	delete(secrets, "BOT_DISCORD_TOKEN") // Remove required secret
	fakeProvider := newFakeSecretProvider(secrets)

	cm := newConfigManagerWithProvider("test-token", logger, fakeProvider)

	err := cm.Reload(context.Background())
	is.True(err != nil)                                     // Should error
	is.True(strings.Contains(err.Error(), "discord token")) // Error should mention missing field
}

func TestConfigManager_Reload_InvalidDuration(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	secrets := createTestSecrets()
	secrets["POLL_PERIOD"] = &doppler.SecretValue{
		Computed: stringPtr("invalid-duration"), // Invalid duration format
	}
	fakeProvider := newFakeSecretProvider(secrets)

	cm := newConfigManagerWithProvider("test-token", logger, fakeProvider)

	err := cm.Reload(context.Background())
	is.True(err != nil)                                   // Should error
	is.True(strings.Contains(err.Error(), "poll_period")) // Error should mention the field
}

func TestConfigManager_Reload_InvalidYAML(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	secrets := createTestSecrets()
	secrets["PLAYERS_YAML"] = &doppler.SecretValue{
		Computed: stringPtr("invalid: yaml: [[["), // Malformed YAML
	}
	fakeProvider := newFakeSecretProvider(secrets)

	cm := newConfigManagerWithProvider("test-token", logger, fakeProvider)

	err := cm.Reload(context.Background())
	is.True(err != nil)                                    // Should error
	is.True(strings.Contains(err.Error(), "PLAYERS_YAML")) // Error should mention PLAYERS_YAML
}

func TestConfigManager_Reload_InvalidKhaleesiThreshold(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	secrets := createTestSecrets()
	secrets["KHALEESI_THRESHOLD"] = &doppler.SecretValue{
		Computed: stringPtr("not-a-number"), // Invalid integer
	}
	fakeProvider := newFakeSecretProvider(secrets)

	cm := newConfigManagerWithProvider("test-token", logger, fakeProvider)

	err := cm.Reload(context.Background())
	is.True(err != nil)                                          // Should error
	is.True(strings.Contains(err.Error(), "khaleesi_threshold")) // Error should mention the field
}

func TestConfigManager_GetThreadSafety(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	fakeProvider := newFakeSecretProvider(createTestSecrets())
	cm := newConfigManagerWithProvider("test-token", logger, fakeProvider)

	// Initial reload
	err := cm.Reload(context.Background())
	is.NoErr(err)

	// Test concurrent reads during a reload
	var wg sync.WaitGroup
	const numReaders = 10

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Concurrent reads should not panic or race
			cfg := cm.Get()
			is.Equal(cfg.DiscordToken, "test-discord-token-12345")
		}()
	}

	// Trigger a reload while reads are happening
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = cm.Reload(context.Background())
	}()

	wg.Wait()
}

func TestConfigManager_LogValueRedactsSecrets(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	cfg := Config{
		PollPeriod:        2 * time.Minute,
		PlayedGap:         30 * time.Minute,
		ChannelID:         "123456789",
		DiscordToken:      "secret-token-should-not-appear",
		OwnerID:           "owner-should-not-appear",
		Players:           []Player{},
		KhaleesiThreshold: nil,
	}

	logValue := cfg.LogValue()
	logStr := logValue.String()

	// Secrets should be redacted
	is.True(!strings.Contains(logStr, "secret-token-should-not-appear"))
	is.True(!strings.Contains(logStr, "owner-should-not-appear"))
	is.True(strings.Contains(logStr, "REDACTED"))

	// Non-secrets should be present
	is.True(strings.Contains(logStr, "123456789")) // channel_id
}

func TestConfigManager_Reload_DopplerError(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	expectedErr := errors.New("doppler API error")
	fakeProvider := newFakeSecretProviderWithError(expectedErr)
	cm := newConfigManagerWithProvider("test-token", logger, fakeProvider)

	err := cm.Reload(context.Background())
	is.True(err != nil)                                               // Should error
	is.True(strings.Contains(err.Error(), "failed to fetch secrets")) // Error should mention Doppler
}

func TestConfigManager_Reload_IntegrationWithDoppler(t *testing.T) {
	t.Parallel()

	// Skip if DOPPLER_TOKEN not available
	dopplerToken := os.Getenv("DOPPLER_TOKEN")
	if dopplerToken == "" {
		t.Skip("DOPPLER_TOKEN is not set")
	}

	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during tests
	}))

	// Create ConfigManager with real Doppler token
	cm, err := NewConfigManager(dopplerToken, logger)
	if err != nil {
		t.Fatalf("failed to create ConfigManager: %v", err)
	}

	// Perform reload from real Doppler API
	err = cm.Reload(context.Background())
	if err != nil {
		t.Fatalf("failed to reload config from Doppler: %v", err)
	}

	// Get config and validate
	cfg := cm.Get()

	// Use matryer/is for assertions
	is := is.New(t)

	// Validate all duration fields are set
	is.True(cfg.PollPeriod > 0) // poll_period must be positive
	is.True(cfg.PlayedGap > 0)  // played_gap must be positive

	// Validate all string fields are not empty
	is.True(cfg.ChannelID != "")    // channel_id must be set
	is.True(cfg.DiscordToken != "") // discord_token must be set
	is.True(cfg.OwnerID != "")      // owner_id must be set

	// Validate optional KhaleesiThreshold (if set, must be >= 0)
	if cfg.KhaleesiThreshold != nil {
		is.True(*cfg.KhaleesiThreshold >= 0) // khaleesi_threshold must be non-negative
	}

	// Validate Players list
	is.True(len(cfg.Players) >= 1) // must have at least 1 player

	// Validate first player has non-empty values
	player := cfg.Players[0]
	is.True(player.Name != "")     // player name must be set
	is.True(player.Tag != "")      // player tag must be set
	is.True(player.Region != "")   // player region must be set
	is.True(player.RealName != "") // player real name must be set

	// Optional: Log success for visibility
	t.Logf("Successfully loaded config from Doppler with %d players", len(cfg.Players))
}
