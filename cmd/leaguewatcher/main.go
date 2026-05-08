package main

import (
	"context"
	"leaguewatcher/internal/leaguewatcher"
	"leaguewatcher/internal/leaguewatcher/bot"
	"leaguewatcher/internal/leaguewatcher/watcher"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	logger.Info("Starting leaguewatcher")
	defer logger.Info("Stopping leaguewatcher")

	ex, err := os.Executable()
	if err != nil {
		logger.Error("Can't get executable path", "error", err)
		return
	}
	exPath := filepath.Dir(ex)
	logger.Info("Executable path", "path", exPath)

	// Initialize ConfigManager with Doppler token
	dopplerToken := os.Getenv("DOPPLER_TOKEN")
	if dopplerToken == "" {
		logger.Error("DOPPLER_TOKEN environment variable is required")
		return
	}

	configMgr, err := leaguewatcher.NewConfigManager(dopplerToken, logger.With("component", "config"))
	if err != nil {
		logger.Error("Failed to create config manager", "error", err)
		return
	}

	// Perform initial config reload from Doppler
	if err := configMgr.Reload(ctx); err != nil {
		logger.Error("Failed to load initial configuration from Doppler", "error", err)
		return
	}

	// Start auto-reload every 5 minutes and capture done channel for graceful shutdown
	autoReloadDone := configMgr.StartAutoReload(ctx, 5*time.Minute)

	// Get initial config
	cfg := configMgr.Get()
	logger.Info("Config loaded from Doppler", slog.Any("config", cfg))

	watcher := watcher.New(
		watcher.Config{
			Period:    cfg.PollPeriod,
			PlayedGap: cfg.PlayedGap,
		},
		configMgr,
		logger.With("component", "watcher"),
	)

	ch, watcherDone := watcher.Run(ctx)

	bot, err := bot.New(
		bot.Config{
			Token:             cfg.DiscordToken,
			OwnerID:           cfg.OwnerID,
			PidorsFile:        filepath.Join(exPath, "pidors.json"),
			LogFile:           filepath.Join(exPath, "log.json"),
			ChannelID:         cfg.ChannelID,
			KhaleesiThreshold: cfg.KhaleesiThreshold,
		},
		ch,
		logger.With("component", "bot"),
	)
	if err != nil {
		logger.Error("Failed to create bot", "error", err)
		return
	}

	botDone, err := bot.Run(ctx)
	if err != nil {
		logger.Error("Error while running bot", "error", err)
		return
	}

	killSignal := make(chan os.Signal, 1)
	signal.Notify(killSignal, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	<-killSignal
	cancel()

	<-autoReloadDone
	<-watcherDone
	<-botDone
}
