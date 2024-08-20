package bot

import (
	"context"
	"fmt"
	"leaguewatcher/internal/khaleesi"
	"leaguewatcher/internal/leaguewatcher"
	"leaguewatcher/internal/leaguewatcher/bot/repository"
	"strings"
	"sync/atomic"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

type Bot struct {
	cfg    Config
	logger *zap.Logger

	matchesCh chan leaguewatcher.Match
	tracks    *TracksMap

	pidors *Pidors

	log *repository.Log

	cnt    atomic.Int32
	thresh atomic.Int32
	kh     *khaleesi.Khaleesi
}

type Config struct {
	Token      string
	OwnerID    string
	PidorsFile string
	LogFile    string
	ChannelID string
}

func New(cfg Config, matchesCh chan leaguewatcher.Match, logger *zap.Logger) (*Bot, error) {
	logger.Info("bot created", zap.Any("config", cfg))

	pidors, err := NewPidors(cfg.PidorsFile)
	if err != nil {
		return nil, err
	}

	bot := Bot{
		cfg:    cfg,
		logger: logger,

		matchesCh: matchesCh,
		tracks:    NewTracksMap(logger.Named("tracks")),
		pidors:    pidors,

		log: repository.NewLog(cfg.LogFile),
	}

	bot.kh, err = khaleesi.New()
	if err != nil {
		return nil, fmt.Errorf("khaleesi: %w", err)
	}
	bot.resetKhaleesi()

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
				b.logger.Debug("match", zap.Any("match", m))
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
		b.khaleesi(ctx, s, m)
	}

	event := leaguewatcher.NewEvent(cmd, fmt.Sprintf("%s %s", m.Author.Username, m.Author.ID))
	err := b.log.AddEvent(event)
	if err != nil {
		b.logger.Warn("failed to log event", zap.Any("event", event), zap.Error(err))
	}
}
