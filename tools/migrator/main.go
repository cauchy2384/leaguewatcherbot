// Command migrator applies SQL schema migrations to the Neon PostgreSQL
// database. It is intentionally a standalone tool (not part of the bot's
// startup path) so schema changes happen only as deliberate operator
// actions.
//
// Configuration resolution order (first match wins):
//
//	-db URL             explicit flag override
//	DATABASE_URL        direct (unpooled) Neon connection string in env
//	DOPPLER_TOKEN       fetches DATABASE_URL from Doppler (project=leaguewatcherbot, config=prd)
//
// Run against production via Doppler CLI:
//
//	doppler run -- go run tools/migrator/main.go up
//
// Or with DOPPLER_TOKEN exported (no Doppler CLI required):
//
//	export DOPPLER_TOKEN=dp.st.…
//	go run tools/migrator/main.go up
//
// MIGRATOR_TEST_DSN is used by integration tests; never for ops.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"leaguewatcher/internal/leaguewatcher/db"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Exit codes. Keep stable: scripts and CI may key off them.
const (
	exitOK              = 0
	exitCLIMisuse       = 1
	exitConfigError     = 2
	exitConnectionError = 3
	exitMigrationError  = 4
	exitLockContention  = 5
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	code := run(os.Args[1:], logger)
	os.Exit(code)
}

func run(args []string, logger *slog.Logger) int {
	if len(args) == 0 {
		usage()
		return exitCLIMisuse
	}

	sub := args[0]
	rest := args[1:]

	fs := flag.NewFlagSet(sub, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbURLFlag := fs.String("db", "", "override $DATABASE_URL (ad-hoc use only; prefer Doppler)")
	verbose := fs.Bool("v", false, "verbose logging")
	if err := fs.Parse(rest); err != nil {
		return exitCLIMisuse
	}
	if *verbose {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	// Special-case: no subcommand === help. We never default-apply.
	switch sub {
	case "up", "down", "status", "version":
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", sub)
		usage()
		return exitCLIMisuse
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dbURL, err := newDatabaseResolver(*dbURLFlag).Resolve(ctx)
	if err != nil {
		logger.Error("failed to resolve DATABASE_URL", "error", err)
		return exitConfigError
	}

	all, err := db.LoadMigrations()
	if err != nil {
		logger.Error("failed to load embedded migrations", "error", err)
		return exitCLIMisuse
	}
	if len(all) == 0 {
		logger.Error("no migrations known to binary")
		return exitCLIMisuse
	}

	handle, err := newPGHandle(ctx, dbURL, logger)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		return exitConnectionError
	}
	defer handle.Close()

	// Parse the optional numeric argument (N) for up/down.
	numArg := fs.Arg(0)
	n := 0
	if numArg != "" {
		v, err := strconv.Atoi(numArg)
		if err != nil {
			logger.Error("invalid numeric argument", "value", numArg, "error", err)
			return exitCLIMisuse
		}
		n = v
	}

	switch sub {
	case "up":
		return cmdUp(ctx, handle, all, n, logger)
	case "down":
		return cmdDown(ctx, handle, all, n, logger)
	case "status":
		return cmdStatus(ctx, handle, all, logger)
	case "version":
		return cmdVersion(ctx, handle, all, logger)
	}
	return exitCLIMisuse
}

func cmdUp(ctx context.Context, h dbHandle, all []db.Migration, target int, logger *slog.Logger) int {
	applied, err := h.ReadApplied(ctx)
	if err != nil {
		logger.Error("failed to read applied set", "error", err)
		return exitMigrationError
	}
	plan := planUp(all, applied, target)
	if len(plan) == 0 {
		logger.Info("schema already up to date")
		return exitOK
	}
	if err := applyPlan(ctx, h, plan, dirUp, logger); err != nil {
		return mapErrToExit(err, logger)
	}
	logger.Info("migrated up", "count", len(plan), "latest", plan[len(plan)-1].Version)
	return exitOK
}

func cmdDown(ctx context.Context, h dbHandle, all []db.Migration, count int, logger *slog.Logger) int {
	applied, err := h.ReadApplied(ctx)
	if err != nil {
		logger.Error("failed to read applied set", "error", err)
		return exitMigrationError
	}
	if count <= 0 {
		count = 1
	}
	plan := planDown(all, applied, count)
	if len(plan) == 0 {
		logger.Info("nothing to roll back")
		return exitOK
	}
	if err := applyPlan(ctx, h, plan, dirDown, logger); err != nil {
		return mapErrToExit(err, logger)
	}
	logger.Info("migrated down", "count", len(plan), "latest_applied", plan[len(plan)-1].Version)
	return exitOK
}

func cmdStatus(ctx context.Context, h dbHandle, all []db.Migration, logger *slog.Logger) int {
	rep, err := readStatus(ctx, h, all)
	if err != nil {
		logger.Error("failed to read status", "error", err)
		return exitMigrationError
	}
	fmt.Print(formatStatus(rep))
	return exitOK
}

func cmdVersion(ctx context.Context, h dbHandle, _ []db.Migration, logger *slog.Logger) int {
	applied, err := h.ReadApplied(ctx)
	if err != nil {
		logger.Error("failed to read applied set", "error", err)
		return exitMigrationError
	}
	max := 0
	for v := range applied {
		if v > max {
			max = v
		}
	}
	fmt.Println(max)
	return exitOK
}

func mapErrToExit(err error, logger *slog.Logger) int {
	switch {
	case errors.Is(err, ErrLockHeld):
		logger.Error("another migrator holds the lock", "error", err)
		return exitLockContention
	default:
		logger.Error("migration failed", "error", err)
		return exitMigrationError
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `migrator — apply SQL schema migrations to the Neon PostgreSQL database

Usage:
  migrator up [N]      apply all pending migrations (or up to version N)
  migrator down [N]    roll back the N most recent migrations (default 1)
  migrator status      print applied and pending migrations
  migrator version     print the highest applied version (0 if none)

Flags:
  -db URL     override $DATABASE_URL (ad-hoc use only)
  -v          verbose logging

Configuration resolution:
  1. -db flag
  2. $DATABASE_URL        (use "doppler run --" to inject via CLI)
  3. $DOPPLER_TOKEN       migrator fetches DATABASE_URL from Doppler
                          (project=leaguewatcherbot, config=prd)

See ADR-010 for design rationale.
`)
}
