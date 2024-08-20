package main

import (
	"context"
	"fmt"
	"leaguewatcher/internal/leaguewatcher"
	"leaguewatcher/internal/leaguewatcher/bot"
	"leaguewatcher/internal/leaguewatcher/watcher"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	rand.Seed(time.Now().UnixNano())

	logger, err := zap.NewDevelopment()
	if err != nil {
		fmt.Println(err)
		return
	}
	logger.Info("Starting leaguewatcher")
	defer logger.Sync()
	defer logger.Info("Stopping leaguewatcher")

	ex, err := os.Executable()
	if err != nil {
		logger.Error("Can't get executable path", zap.Error(err))
		return
	}
	exPath := filepath.Dir(ex)
	logger.Info("Executable path", zap.String("path", exPath))

	fd, err := os.Open(filepath.Join(exPath, "config.yaml"))
	if err != nil {
		logger.Error("Can't open config file", zap.Error(err))
		return
	}
	defer fd.Close()

	var cfg leaguewatcher.Config
	err = yaml.NewDecoder(fd).Decode(&cfg)
	if err != nil {
		logger.Error("Can't decode config file", zap.Error(err))
		return
	}
	if err := cfg.IsValid(); err != nil {
		logger.Error("Config is invalid", zap.Error(err))
		return
	}
	logger.Info("Config loaded", zap.Any("config", cfg))

	watcher := watcher.New(
		watcher.Config{
			Period:    cfg.PollPeriod,
			PlayedGap: cfg.PlayedGap,
			Players:   cfg.Players,
		},
		logger.Named("watcher"),
	)

	ch, watcherDone := watcher.Run(ctx)

	bot, err := bot.New(
		bot.Config{
			Token:      os.Getenv("BOT_DISCORD_TOKEN"),
			OwnerID:    os.Getenv("BOT_OWNER_ID"),
			PidorsFile: filepath.Join(exPath, "pidors.json"),
			LogFile:    filepath.Join(exPath, "log.json"),
			ChannelID:  cfg.ChannelID,
		},
		ch,
		logger.Named("bot"),
	)
	if err != nil {
		logger.Error("Failed to create bot", zap.Error(err))
		return
	}

	botDone, err := bot.Run(ctx)
	if err != nil {
		logger.Error("Error while running bot", zap.Error(err))
		return
	}

	killSignal := make(chan os.Signal, 1)
	signal.Notify(killSignal, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	<-killSignal
	cancel()

	<-watcherDone
	<-botDone
}
