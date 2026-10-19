package main

import (
	"context"
	"leaguewatcher/internal/leaguewatcher"
	"leaguewatcher/internal/leaguewatcher/bot"
	"leaguewatcher/internal/leaguewatcher/bot/repository"
	"leaguewatcher/internal/leaguewatcher/db"
	"leaguewatcher/internal/leaguewatcher/neonauth"
	"leaguewatcher/internal/leaguewatcher/watcher"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata" // bake IANA zones (Europe/Moscow) into the binary; no host tzdata dependency

	"github.com/jackc/pgx/v5/pgxpool"
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

	// Connect to Neon PostgreSQL. Required since ADR-012: pidor commands
	// write to the pidor_rolls table and the bot refuses to start without a DB.
	var pool *pgxpool.Pool
	databaseURL := cfg.DatabaseURLPooled
	if databaseURL == "" {
		databaseURL = cfg.DatabaseURL
	}
	if databaseURL == "" {
		logger.Error("DATABASE_URL is required (see ADR-012: pidor store moved to Postgres)")
		return
	}
	pool, err = db.Connect(ctx, databaseURL, logger.With("component", "db"))
	if err != nil {
		logger.Error("Failed to connect to database", "error", err)
		return
	}
	defer pool.Close()

	// Pidor storage: Postgres only (ADR-012). Europe/Moscow is the canonical
	// "today" zone, per ADR-011.
	mskLoc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		logger.Error("Failed to load Europe/Moscow timezone", "error", err)
		return
	}
	pidorStore, err := repository.NewPidorStorePostgres(pool)
	if err != nil {
		logger.Error("Failed to construct pidor store", "error", err)
		return
	}
	pidors, err := bot.NewPidors(pidorStore, mskLoc)
	if err != nil {
		logger.Error("Failed to construct pidor service", "error", err)
		return
	}
	logger.Info("pidor store ready", "backend", "postgres", "location", mskLoc.String())

	// Neon Auth reachability check (non-fatal per ADR-009)
	if cfg.NeonAuthBaseURL != "" {
		if err := neonauth.CheckReachable(ctx, cfg.NeonAuthBaseURL); err != nil {
			logger.Warn("Neon Auth reachability check failed", "error", err)
		} else {
			logger.Info("Neon Auth reachable")
		}
	} else {
		logger.Warn("NEON_AUTH_BASE_URL is empty; Neon Auth integration disabled")
	}

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
			LogFile:           filepath.Join(exPath, "log.json"),
			ChannelID:         cfg.ChannelID,
			KhaleesiThreshold: cfg.KhaleesiThreshold,
		},
		configMgr,
		ch,
		pidors,
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

	// Stop the FlareSolverr background fetcher
	api := watcher.GetAPI()
	if api != nil {
		api.Stop()
	}

	<-autoReloadDone
	<-watcherDone
	<-botDone
}
