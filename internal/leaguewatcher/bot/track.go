package bot

import (
	"context"
	"fmt"
	"leaguewatcher/internal/leaguewatcher"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/kyokomi/emoji/v2"
	"go.uber.org/zap"
)

type Track struct {
	channelID string
	done      chan struct{}
	msgs      chan leaguewatcher.Match
}

func NewTrack(channelID string) Track {
	return Track{
		channelID: channelID,
		done:      make(chan struct{}),
		msgs:      make(chan leaguewatcher.Match),
	}
}

type TracksMap struct {
	logger *zap.Logger
	mu     sync.Mutex
	tracks map[string]Track
}

func NewTracksMap(logger *zap.Logger) *TracksMap {
	return &TracksMap{
		logger: logger,
		mu:     sync.Mutex{},
		tracks: make(map[string]Track),
	}
}

func (t *TracksMap) Channels() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	ss := make([]string, 0, len(t.tracks))
	for _, track := range t.tracks {
		ss = append(ss, track.channelID)
	}

	return ss
}

func (t *TracksMap) IsTracking(channelID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	_, ok := t.tracks[channelID]
	return ok
}

func (t *TracksMap) Track(ctx context.Context, s *discordgo.Session, cID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	track := NewTrack(cID)
	t.tracks[cID] = track

	go func() {
		var err error

		defer func() {
			message := fmt.Sprintf("We will never be slaves! %s", emoji.Sprint(":mobile_phone_off:"))
			_, err := s.ChannelMessageSend(cID, message)
			if err != nil {
				t.logger.Warn("failed to send message", zap.String("channel", cID), zap.Error(err))
				return
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-track.done:
				return
			case m, ok := <-track.msgs:
				if !ok {
					return
				}
				t.logger.Debug("match", zap.Any("match", m))

				msg := matchToMessage(m)
				_, err = s.ChannelMessageSendComplex(cID, &msg)
				if err != nil {
					t.logger.Warn("failed to send message", zap.String("channel", cID), zap.Error(err))
				}
			}
		}
	}()
}

func (t *TracksMap) Untrack(channelID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.untrack(channelID)
}

func (t *TracksMap) untrack(channelID string) {
	track, ok := t.tracks[channelID]
	if !ok {
		return
	}

	close(track.msgs)
	close(track.done)
	delete(t.tracks, channelID)
}

func (t *TracksMap) UntrackAll() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for id := range t.tracks {
		t.untrack(id)
	}
}

func (t *TracksMap) Fanout(m leaguewatcher.Match) {
	t.mu.Lock()
	defer t.mu.Unlock()

	ticker := time.NewTimer(10 * time.Second)
	defer ticker.Stop()

	for _, track := range t.tracks {
		ticker.Reset(10 * time.Second)
		select {
		case track.msgs <- m:
			t.logger.Debug("fanout", zap.String("channel", track.channelID))
		case <-ticker.C:
			t.logger.Warn("fanout channel is stuck", zap.String("channel", track.channelID))
		}
	}
}

func (b *Bot) track(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {

	cID := m.ChannelID

	if !strings.EqualFold(m.Author.Username, b.cfg.OwnerID) {
		_, _ = s.ChannelMessageSend(cID, emoji.Sprint(":poop:"))
		b.logger.Info("not owner", zap.String("channel", cID),
			zap.String("id", m.Author.ID), zap.String("global", m.Author.GlobalName), zap.String("username", m.Author.Username),
			zap.String("expected", b.cfg.OwnerID),
		)
		return
	}

	if b.tracks.IsTracking(cID) {
		b.logger.Info("channel is already tracked", zap.String("channel", cID))
		return
	}

	b.tracks.Track(ctx, s, cID)
	b.logger.Info("channel tracked", zap.String("channel", cID))
	b.logger.Debug("tracked channels", zap.Strings("channels", b.tracks.Channels()))

	message := fmt.Sprintf("Yes, master? %s", emoji.Sprint(":on:"))
	_, err := s.ChannelMessageSend(cID, message)
	if err != nil {
		b.logger.Warn("failed to send message", zap.String("channel", m.ChannelID), zap.Error(err))
		return
	}
}

func (b *Bot) untrack(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {

	cID := m.ChannelID

	if !strings.EqualFold(m.Author.Username, b.cfg.OwnerID) {
		_, _ = s.ChannelMessageSend(cID, emoji.Sprint(":poop:"))
		b.logger.Info("not owner", zap.String("channel", m.ChannelID), zap.String("author", m.Author.String()))
		return
	}

	if !b.tracks.IsTracking(cID) {
		b.logger.Info("channel is not tracked", zap.String("channel", m.ChannelID))
		return
	}

	b.tracks.Untrack(cID)
	b.logger.Info("channel is untracked", zap.String("channel", m.ChannelID))
	b.logger.Debug("tracked channels", zap.Strings("channels", b.tracks.Channels()))
}

func matchToMessage(m leaguewatcher.Match) discordgo.MessageSend {

	action := action(m.Win)
	emo := smiley(m.Win)
	content := fmt.Sprintf("%s %s %s", m.Player.RealName, action, emo)
	if m.LP != nil {
		content = fmt.Sprintf("%s\n%+d LP!", content, *m.LP)
	}

	if m.Role == "UNKNOWN" {
		m.Role = ""
	}
	urlTitle := fmt.Sprintf("[%s] %s %s - %d/%d/%d", m.Queue, m.Champion.Name, m.Role, m.Kills, m.Deaths, m.Assists)

	msg := discordgo.MessageSend{
		Content: content,
		Embed: &discordgo.MessageEmbed{
			Type:      discordgo.EmbedTypeLink,
			URL:       m.URL(),
			Title:     urlTitle,
			Timestamp: m.FinishedAt().Format(time.RFC3339),
			Color:     0x00ff00,
		},
	}

	return msg
}

func action(win bool) string {
	if win {
		return pickRand([]string{
			"затащил",
			"boss",
			"слишком хорош",
			"наша гордость",
			"мамкина радость",
			"супергейрой",
			"срце moje",
			"душо moja",
			"jунак",
		})
	}
	return pickRand([]string{
		"соснул",
		"slave",
		"кукарек",
		"позорище",
		"марионетка запада",
		"kurac",
		"sranje",
		"pička",
		"drkač",
		"čmar",
	})
}

func emojiWin() []string {
	return []string{
		emoji.Sprint(":muscle:"),
		emoji.Sprint(":lion:"),
		emoji.Sprint(":fire:"),
		emoji.Sprint(":scream:"),
		emoji.Sprint(":military_medal:"),
		"<:Ah:686344364506742819>",
		"<:gigachad:901123302553169930>",
		emoji.Sprint(":tiger:"),
		emoji.Sprint(":eggplant:"),
		emoji.Sprint(":beer:"),
		emoji.Sprint(":flag_ru:"),
	}
}

func emojiLoose() []string {
	return []string{
		emoji.Sprint(":poop:"),
		emoji.Sprint(":clown:"),
		emoji.Sprint(":lobster:"),
		emoji.Sprint(":crying_cat_face:"),
		emoji.Sprint(":crab:"),
		emoji.Sprint(":see_no_evil:"),
		"<:FeelsBadMan:690914614489120822>",
		"<:B5589_PutinFacepalms:690914614770270268>",
		emoji.Sprint(":cucumber:"),
		emoji.Sprint(":wheelchair:"),
		emoji.Sprint(":rainbow_flag:"),
	}
}

func smiley(win bool) string {
	if win {
		return pickRand(emojiWin())
	}
	return pickRand(emojiLoose())
}

func pickRand(ss []string) string {
	randomIndex := rand.Intn(len(ss))
	return ss[randomIndex]
}
