package bot

import (
	"context"
	"fmt"
	"leaguewatcher/internal/khaleesi"
	"leaguewatcher/internal/leaguewatcher"
	"leaguewatcher/internal/leaguewatcher/bot/repository"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/bwmarrin/discordgo"
)

type Bot struct {
	cfg       Config
	configMgr *leaguewatcher.ConfigManager
	logger    *slog.Logger

	matchesCh chan leaguewatcher.Match
	tracks    *TracksMap

	pidors *Pidors

	log *repository.Log

	cnt    atomic.Int32
	thresh atomic.Int32
	kh     *khaleesi.Khaleesi
}

type Config struct {
	Token             string
	OwnerID           string
	PidorsFile        string
	LogFile           string
	ChannelID         string
	KhaleesiThreshold *int
}

// LogValue implements slog.LogValuer to prevent secrets from being logged
func (cfg Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("token", "***REDACTED***"),
		slog.String("owner_id", "***REDACTED***"),
		slog.String("pidors_file", cfg.PidorsFile),
		slog.String("log_file", cfg.LogFile),
		slog.String("channel_id", cfg.ChannelID),
		slog.Any("khaleesi_threshold", cfg.KhaleesiThreshold),
	)
}

func New(cfg Config, configMgr *leaguewatcher.ConfigManager, matchesCh chan leaguewatcher.Match, logger *slog.Logger) (*Bot, error) {
	logger.Info("bot created", slog.Any("config", cfg))

	pidors, err := NewPidors(cfg.PidorsFile)
	if err != nil {
		return nil, err
	}

	bot := Bot{
		cfg:       cfg,
		configMgr: configMgr,
		logger:    logger,

		matchesCh: matchesCh,
		tracks:    NewTracksMap(logger.With("component", "tracks")),
		pidors:    pidors,

		log: repository.NewLog(cfg.LogFile),
	}

	if cfg.KhaleesiThreshold != nil && *cfg.KhaleesiThreshold > 0 {
		bot.kh, err = khaleesi.New()
		if err != nil {
			return nil, fmt.Errorf("khaleesi: %w", err)
		}
		bot.resetKhaleesi()
		logger.Info("khaleesi enabled", "threshold", *cfg.KhaleesiThreshold)
	} else {
		logger.Info("khaleesi disabled")
	}

	return &bot, nil
}

func (b *Bot) Run(ctx context.Context) (chan struct{}, error) {
	done := make(chan struct{})

	dg, err := discordgo.New("Bot " + b.cfg.Token)
	if err != nil {
		return nil, err
	}

	dg.AddHandler(func(s *discordgo.Session, event *discordgo.Ready) {
		b.ready(s, event)
	})
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		b.cmd(ctx, s, m)
	})

	if err := dg.Open(); err != nil {
		return nil, err
	}
	b.logger.Info("discord bot opened")

	go func() {
		defer close(done)
		defer b.logger.Info("discord bot closed")
		defer dg.Close()
		defer b.tracks.UntrackAll()

		b.tracks.Track(ctx, dg, b.cfg.ChannelID)

		for {
			select {
			case <-ctx.Done():
				return
			case m := <-b.matchesCh:
				b.logger.Info("match received on channel", "player", m.Player.RealName, "id", m.ID, "queue", m.Queue, "kda", fmt.Sprintf("%d/%d/%d", m.Kills, m.Deaths, m.Assists), "lp", m.LP, "win", m.Win)
				b.tracks.Fanout(m)
			}
		}
	}()

	return done, nil
}

func (b *Bot) ready(s *discordgo.Session, _ *discordgo.Ready) {
	s.UpdateGameStatus(0, "legendary slave")
	b.logger.Info("discord bot ready")
}

func (b *Bot) cmd(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {

	if m.Author.ID == s.State.User.ID {
		return
	}

	content := strings.TrimSpace(m.Content)
	mention := fmt.Sprintf("<@%s>", s.State.User.ID)
	mentionNick := fmt.Sprintf("<@!%s>", s.State.User.ID)

	isMentioned := false
	query := ""
	if strings.HasPrefix(content, mention) {
		isMentioned = true
		query = strings.TrimSpace(strings.TrimPrefix(content, mention))
	} else if strings.HasPrefix(content, mentionNick) {
		isMentioned = true
		query = strings.TrimSpace(strings.TrimPrefix(content, mentionNick))
	}

	cmd := m.Content
	switch {
	case strings.EqualFold(cmd, "!info"):
		b.info(ctx, s, m)
	case strings.EqualFold(cmd, "!track"):
		b.track(ctx, s, m)
	case strings.EqualFold(cmd, "!untrack"):
		b.untrack(ctx, s, m)
	case strings.EqualFold(cmd, "!pidor"):
		b.pidor(ctx, s, m)
	case strings.EqualFold(cmd, "!pidorday"):
		b.pidorOfTheDay(ctx, s, m)
	case strings.EqualFold(cmd, "!pidorstats"):
		b.pidorStats(ctx, s, m)
	case strings.EqualFold(cmd, "!pidorok"):
		b.pidorPersonalStats(ctx, s, m)
	default:
		if isMentioned && query != "" {
			b.ask(ctx, s, m, query)
		} else {
			b.khaleesi(ctx, s, m)
		}
	}

	event := leaguewatcher.NewEvent(cmd, fmt.Sprintf("%s %s", m.Author.Username, m.Author.ID))
	err := b.log.AddEvent(event)
	if err != nil {
		b.logger.Warn("failed to log event", slog.Any("event", event), "error", err)
	}
}
