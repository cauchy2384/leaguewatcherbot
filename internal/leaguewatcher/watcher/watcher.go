package watcher

import (
	"context"
	"fmt"
	"leaguewatcher/internal/leaguewatcher"
	"leaguewatcher/internal/leaguewatcher/watcher/mobalytics"
	"leaguewatcher/internal/leaguewatcher/watcher/mobalytics/flaresolverr"
	"leaguewatcher/internal/leaguewatcher/watcher/repository"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

type Watcher struct {
	period    time.Duration
	playedGap time.Duration
	configMgr ConfigProvider
	logger    *slog.Logger

	api   *mobalytics.Client
	store *repository.Match

	// errMu protects errState.
	// key: "region/name/tag", value: last known state (ok or error)
	errMu    sync.RWMutex
	errState map[string]string // "ok" or "error"
}

// ConfigProvider provides access to the current configuration
type ConfigProvider interface {
	GetPlayers() []leaguewatcher.Player
	Get() leaguewatcher.Config
}

type Config struct {
	Period    time.Duration
	PlayedGap time.Duration
}

func New(cfg Config, configMgr ConfigProvider, logger *slog.Logger) *Watcher {
	logger.Info("creating watcher", slog.Any("config", cfg))

	if cfg.Period == 0 {
		cfg.Period = 1 * time.Minute
	}
	if cfg.PlayedGap == 0 {
		cfg.PlayedGap = 15 * time.Minute
	}

	return &Watcher{
		period:    cfg.Period,
		playedGap: cfg.PlayedGap,
		configMgr: configMgr,
		logger:    logger,
		api:       mobalytics.NewClient(logger.With("component", "api"), flaresolverr.NewClient(logger.With("component", "flare"))),
		store:     repository.NewMatch(),
		errState:  make(map[string]string),
	}
}

// shouldReportError returns true if we should report an error in Discord.
// Errors are only shown once (OK→ERROR transition), not on consecutive errors.
// A successful response resets the state, so the next error will be reported again.
func (w *Watcher) shouldReportError(playerKey string) bool {
	w.errMu.Lock()
	defer w.errMu.Unlock()

	last := w.errState[playerKey]
	if last == "error" {
		// Already reported this error, keep quiet
		return false
	}
	// First error or recovery — report it
	w.errState[playerKey] = "error"
	return true
}

// reportOK resets the error state for a player after a successful response.
// This ensures the next error will be reported again.
func (w *Watcher) reportOK(playerKey string) {
	w.errMu.Lock()
	defer w.errMu.Unlock()
	w.errState[playerKey] = "ok"
}

func (w *Watcher) Run(ctx context.Context) (chan leaguewatcher.Match, chan struct{}) {
	done := make(chan struct{})
	players := w.configMgr.GetPlayers()
	ch := make(chan leaguewatcher.Match, len(players))

	// Retry the initial sync — FlareSolverr may still be starting up
	// (connection refused). Keep the channel open and retry with a delay
	// instead of bailing out.
	const maxSyncAttempts = 5
	syncAttempts := 0
	for {
		if err := w.api.Sync(ctx); err != nil {
			syncAttempts++
			w.logger.Error("failed to sync, retrying", "attempt", syncAttempts, "error", err)
			if syncAttempts >= maxSyncAttempts {
				close(ch)
				close(done)
				return ch, done
			}
			select {
			case <-time.After(15 * time.Second):
			case <-ctx.Done():
				close(ch)
				close(done)
				return ch, done
			}
			continue
		}
		break
	}

	ticker := time.NewTicker(w.period)

	go func() {
		defer close(done)
		defer w.logger.Info("watcher stopped")
		defer ticker.Stop()
		defer close(ch)

		for {
			select {
			case <-ticker.C:
				w.checkPlayers(ctx, ch)
			case <-ctx.Done():
				return
			}
		}
	}()

	w.logger.Info("watcher started")
	return ch, done
}

// GetAPI returns the mobalytics client for external use (e.g., graceful shutdown).
func (w *Watcher) GetAPI() *mobalytics.Client {
	return w.api
}

func (w *Watcher) checkPlayers(ctx context.Context, ch chan leaguewatcher.Match) {
	wg, ctx := errgroup.WithContext(ctx)

	// Get the latest player list from ConfigManager on each iteration
	players := w.configMgr.GetPlayers()

	for i := range players {
		player := players[i]
		wg.Go(func() error {
			ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
			defer cancel()

			// TODO: Profile refresh disabled - Mobalytics API changed, needs investigation
			// See ADR-003 for details and plan to re-enable
			// w.logger.Debug("refreshing player", "player", player.Name)
			// status, err := w.api.RefreshProfile(ctx, player.Region, player.Name, player.Tag)
			// if err != nil {
			// 	w.logger.Warn("failed to refresh", "player", player.Name, "error", err)
			// } else {
			// 	w.logger.Debug("refreshed", "player", player.Name, "status", status)
			// }

			w.logger.Debug("checking player", "player", player.Name)
			matches, err := w.api.Matches(ctx, player.Region, player.Name, player.Tag)
			if err != nil {
				w.logger.Error("failed to get matches", "player", player.Name, "error", err)
				playerKey := fmt.Sprintf("%s/%s/%s", player.Region, player.Name, player.Tag)
				if w.shouldReportError(playerKey) {
					// Send error signal to bot — it will report once in Discord
					select {
					case ch <- leaguewatcher.Match{
						Player: player,
						Queue:  "ERROR",
					}:
						w.logger.Info("error reported to discord", "player", player.Name)
					default:
					}
				}
				return nil
			}
			w.logger.Info("matches fetched", "player", player.Name, "count", len(matches))

			playerKey := fmt.Sprintf("%s/%s/%s", player.Region, player.Name, player.Tag)
			w.reportOK(playerKey)
			for i, m := range matches {
				w.logger.Debug("match detail", "player", player.Name, "idx", i, "id", m.ID, "queue", m.Queue, "kda", fmt.Sprintf("%d/%d/%d", m.Kills, m.Deaths, m.Assists), "lp", m.LP, "win", m.Win, "started_at", m.StartedAt)
			}
			if len(matches) == 0 {
				w.logger.Debug("no matches found", "player", player.Name)
				return nil
			}

			match := matches[0]

			if match.FinishedAt().Add(w.playedGap).Before(time.Now()) {
				w.logger.Debug("match is too old", "player", player.Name, slog.Time("finished_at", match.FinishedAt()))
				return nil
			}

			lastMatchID, ok := w.store.Get(player.Region, player.Name)
			if ok && lastMatchID == match.ID {
				w.logger.Debug("match is already processed", "player", player.Name, "match_id", match.ID)
				return nil
			}

			w.logger.Info("match found", "player", player.Name, "match_id", match.ID)
			match.Player.RealName = player.RealName
			ch <- match

			w.store.Set(player.Region, player.Name, match.ID)

			return nil
		})
	}

	wg.Wait()
}
