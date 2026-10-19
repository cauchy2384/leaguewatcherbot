// Command pidors-backfill imports pidors.json aggregate stats into the
// pidor_rolls Postgres table. It is a one-shot operator tool intended
// to run BEFORE the bot starts consuming pidor_rolls directly (see
// ADR-011 §"Backfill strategy" for the algorithm and the "Called"
// decision).
//
//	# Dry-run against the production DB (read-only)
//	export DOPPLER_TOKEN=dp.st.…
//	go run tools/pidors-backfill/main.go -dry-run -v
//
//	# Real import
//	go run tools/pidors-backfill/main.go
//
//	# Re-run is idempotent: rows already present are reported as "skipped".
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"leaguewatcher/internal/leaguewatcher/bot/repository"
	"leaguewatcher/internal/leaguewatcher/db"
	"log/slog"
	"os"
	"time"
)

// Exit codes mirror tools/migrator so operational scripts can treat
// both tools uniformly.
const (
	exitOK              = 0
	exitCLIMisuse       = 1
	exitConfigError     = 2
	exitConnectionError = 3
	exitApplyError      = 4
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	os.Exit(run(os.Args[1:], logger))
}

func run(args []string, logger *slog.Logger) int {
	fs := flag.NewFlagSet("pidors-backfill", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonPath := fs.String("json", "./pidors.json", "path to pidors.json")
	dbFlag := fs.String("db", "", "override $DATABASE_URL (ad-hoc use only)")
	backupName := fs.String("backup-name", "",
		"override the backup table name (default: pidor_rolls_backup_<UTC timestamp>)")
	dryRun := fs.Bool("dry-run", false, "compute plan and print it, do not write to DB")
	verbose := fs.Bool("v", false, "verbose logging (per-row details)")
	if err := fs.Parse(args); err != nil {
		return exitCLIMisuse
	}
	if *verbose {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	p, loadWarnings, err := loadPidors(*jsonPath)
	if err != nil {
		logger.Error("failed to load pidors.json", "path", *jsonPath, "error", err)
		return exitCLIMisuse
	}
	for _, w := range loadWarnings {
		logger.Warn("load warning", "reason", w)
	}

	rows, warnings := computePlan(p)
	for _, w := range warnings {
		logger.Warn("skipped entry", "reason", w)
	}

	if len(rows) == 0 {
		logger.Info("no rows to import", "source", *jsonPath)
		return exitOK
	}

	printPlanSummary(rows, logger)
	if *verbose {
		printPlanDetail(rows, logger)
	}

	if *dryRun {
		logger.Info("dry-run complete; nothing written to DB", "prepared_rows", len(rows))
		return exitOK
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dsn, err := db.NewDSNResolver(*dbFlag).Resolve(ctx)
	if err != nil {
		logger.Error("failed to resolve DATABASE_URL", "error", err)
		return exitConfigError
	}

	pool, err := db.Connect(ctx, dsn, logger.With("component", "backfill-db"))
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		return exitConnectionError
	}
	defer pool.Close()

	name := *backupName
	if name == "" {
		name = defaultBackupName(time.Now())
	}
	logger.Info("snapshotting pidor_rolls before truncate", "backup_table", name)

	inserted, truncated, err := applyPlan(ctx, pool, "pidor_rolls", name, rows)
	if err != nil {
		logger.Error("backfill failed", "error", err, "backup_table", name)
		return exitApplyError
	}

	logger.Info("backfill complete",
		"inserted", inserted,
		"truncated", truncated,
		"total_planned", len(rows),
		"backup_table", name,
	)
	return exitOK
}

// loadPidors parses the pidors.json file. Matches the bot's own reader
// (internal/leaguewatcher/bot/repository/pidor.go NewPidor) which uses
// a streaming Decoder and stops at the first complete JSON value —
// trailing data after the closing brace is ignored with a warning.
// json.Unmarshal would refuse the real prod file, which has trailing
// whitespace/garbage from a previous bot-write bug.
func loadPidors(path string) (*repository.Pidor, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var p repository.Pidor
	if err := dec.Decode(&p); err != nil {
		return nil, nil, fmt.Errorf("decode: %w", err)
	}
	var warnings []string
	// Anything left in the decoder? More() reports whether the stream
	// has additional values beyond the one we just decoded.
	if dec.More() {
		warnings = append(warnings,
			"pidors.json has trailing data after the main JSON document; ignoring it")
	}
	return &p, warnings, nil
}

func printPlanSummary(rows []planRow, logger *slog.Logger) {
	perChannel := make(map[string]int)
	perChannelUsers := make(map[string]int)
	totalRolls := 0
	for _, r := range rows {
		perChannel[r.ChannelID] += r.TimesRolled
		perChannelUsers[r.ChannelID]++
		totalRolls += r.TimesRolled
	}
	logger.Info("backfill plan",
		"rows", len(rows),
		"channels", len(perChannel),
		"total_rolls", totalRolls,
	)
	for ch, n := range perChannel {
		logger.Info("  channel", "id", ch, "users", perChannelUsers[ch], "rolls", n)
	}
}

func printPlanDetail(rows []planRow, logger *slog.Logger) {
	for i, r := range rows {
		logger.Debug("  row",
			"i", i,
			"channel", r.ChannelID,
			"user_id", r.UserID,
			"user_name", r.UserName,
			"rolled_on", r.RolledOn.Format("2006-01-02"),
			"times_rolled", r.TimesRolled,
		)
	}
}
