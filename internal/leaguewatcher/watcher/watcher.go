package watcher

import (
	"context"
	"leaguewatcher/internal/leaguewatcher"
	"leaguewatcher/internal/leaguewatcher/watcher/mobalytics"
	"leaguewatcher/internal/leaguewatcher/watcher/repository"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"
)

type Watcher struct {
	cfg    Config
	logger *slog.Logger

	api   *mobalytics.Client
	store *repository.Match
}

type Config struct {
	Period    time.Duration
	PlayedGap time.Duration

	Players []leaguewatcher.Player
}

func New(cfg Config, logger *slog.Logger) *Watcher {
	logger.Info("creating watcher", slog.Any("config", cfg))

	if cfg.Period == 0 {
		cfg.Period = 1 * time.Minute
	}
	if cfg.PlayedGap == 0 {
		cfg.PlayedGap = 15 * time.Minute
	}

	return &Watcher{
		cfg:    cfg,
		logger: logger,
		api:    mobalytics.NewClient(logger.With("component", "api")),
		store:  repository.NewMatch(),
	}
}

func (w *Watcher) Run(ctx context.Context) (chan leaguewatcher.Match, chan struct{}) {
	done := make(chan struct{})
	ch := make(chan leaguewatcher.Match, len(w.cfg.Players))

	if err := w.api.Sync(ctx); err != nil {
		w.logger.Error("failed to sync", "error", err)
		close(ch)
		close(done)
		return ch, done
	}

	ticker := time.NewTicker(w.cfg.Period)

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

func (w *Watcher) checkPlayers(ctx context.Context, ch chan leaguewatcher.Match) {
	wg, ctx := errgroup.WithContext(ctx)

	for i := range w.cfg.Players {
		player := w.cfg.Players[i]
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
				return err
			}
			if len(matches) == 0 {
				w.logger.Debug("no matches found", "player", player.Name)
				return nil
			}

			match := matches[0]

			if match.FinishedAt().Add(w.cfg.PlayedGap).Before(time.Now()) {
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
