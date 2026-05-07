package main

import (
	"context"
	"leaguewatcher/internal/leaguewatcher"
	"leaguewatcher/internal/leaguewatcher/bot"
	"leaguewatcher/internal/leaguewatcher/watcher"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	rand.Seed(time.Now().UnixNano())

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

	fd, err := os.Open(filepath.Join(exPath, "config.yaml"))
	if err != nil {
		logger.Error("Can't open config file", "error", err)
		return
	}
	defer fd.Close()

	var cfg leaguewatcher.Config
	err = yaml.NewDecoder(fd).Decode(&cfg)
	if err != nil {
		logger.Error("Can't decode config file", "error", err)
		return
	}
	if err := cfg.IsValid(); err != nil {
		logger.Error("Config is invalid", "error", err)
		return
	}
	logger.Info("Config loaded", slog.Any("config", cfg))

	watcher := watcher.New(
		watcher.Config{
			Period:    cfg.PollPeriod,
			PlayedGap: cfg.PlayedGap,
			Players:   cfg.Players,
		},
		logger.With("component", "watcher"),
	)

	ch, watcherDone := watcher.Run(ctx)

	bot, err := bot.New(
		bot.Config{
			Token:             os.Getenv("BOT_DISCORD_TOKEN"),
			OwnerID:           os.Getenv("BOT_OWNER_ID"),
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

	<-watcherDone
	<-botDone
}
