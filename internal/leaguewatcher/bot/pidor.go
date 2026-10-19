package bot

import (
	"cmp"
	"context"
	"fmt"
	"leaguewatcher/internal/leaguewatcher/bot/repository"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/kyokomi/emoji/v2"
)

const (
	pidorStatsLen = 10

	// msgDBUnavailable is the user-facing message for storage errors.
	// Fail-closed: we do not roll or read stats when the DB is down.
	msgDBUnavailable = "У меня проблемы с подключением к БД. Попробуй позже."
)

func (b *Bot) pidor(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {
	_, err := s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s, ты пидор!", m.Author.Mention()))
	if err != nil {
		return
	}
}

// Pidors wraps a PidorStore with bot-specific state: the timezone used
// to derive "today" (per ADR-011: Europe/Moscow).
type Pidors struct {
	store repository.PidorStore
	loc   *time.Location
}

// NewPidors constructs a Pidors bound to the given store and location.
// location must be non-nil — the caller is expected to have resolved
// Europe/Moscow at startup and refused to boot on failure.
func NewPidors(store repository.PidorStore, loc *time.Location) (*Pidors, error) {
	if store == nil {
		return nil, fmt.Errorf("repository.PidorStore is required")
	}
	if loc == nil {
		return nil, fmt.Errorf("time.Location is required (per ADR-011 use Europe/Moscow)")
	}
	return &Pidors{store: store, loc: loc}, nil
}

// today returns the current calendar date at UTC midnight in the
// configured zone. Format consumed by Postgres as `::date`.
func (p *Pidors) today() time.Time {
	now := time.Now().In(p.loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func (b *Bot) pidorOfTheDay(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {
	cID := m.ChannelID

	c, err := s.State.Channel(cID)
	if err != nil {
		b.logger.Warn("failed to get channel", "channel", cID, "error", err)
		return
	}
	gID := c.GuildID
	members, err := s.GuildMembers(gID, "0", 1000)
	if err != nil {
		b.logger.Warn("failed to get guild members", "guild", gID, "error", err)
		return
	}

	var candidates []*discordgo.Member
	for _, mem := range members {
		if mem.User.Bot {
			continue
		}
		candidates = append(candidates, mem)
	}
	if len(candidates) == 0 {
		b.logger.Warn("no eligible members for pidorday", "channel", cID)
		return
	}

	winner := candidates[rand.IntN(len(candidates))]
	b.logger.Debug("pidor of the day candidate", "user", winner.User.Username)

	// Race-safe reservation. The DB's UNIQUE(channel_id, rolled_on)
	// guarantees only one winner per (channel, day) globally; we
	// don't need a "check first" round trip.
	won, err := b.pidors.store.TryChooseToday(ctx, cID, winner.User.ID, winner.User.Username, b.pidors.today())
	if err != nil {
		b.logger.Error("pidor TryChooseToday failed", "channel", cID, "error", err)
		_, _ = s.ChannelMessageSend(cID, msgDBUnavailable)
		return
	}
	if !won {
		_, err := s.ChannelMessageSend(cID,
			fmt.Sprintf("%s, сегодня уже выбирали, пидрила!", m.Author.Mention()))
		if err != nil {
			return
		}
		return
	}

	_, err = s.ChannelMessageSend(c.ID,
		fmt.Sprintf(
			"Пидор дня сегодня... %s! %s",
			winner.Mention(),
			emoji.Sprintf(":rooster:"),
		))
	if err != nil {
		return
	}
}

func (b *Bot) pidorStats(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {
	stats, err := b.pidors.store.Stats(ctx, m.ChannelID)
	if err != nil {
		b.logger.Error("pidor Stats failed", "channel", m.ChannelID, "error", err)
		_, _ = s.ChannelMessageSend(m.ChannelID, msgDBUnavailable)
		return
	}
	b.sendPidorStatsRender(s, m.ChannelID, stats)
}

func (b *Bot) pidorPersonalStats(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {
	stats, err := b.pidors.store.Stats(ctx, m.ChannelID)
	if err != nil {
		b.logger.Error("pidor Stats failed", "channel", m.ChannelID, "error", err)
		_, _ = s.ChannelMessageSend(m.ChannelID, msgDBUnavailable)
		return
	}

	v, ok := stats[m.Author.ID]
	if !ok {
		_, err := s.ChannelMessageSend(
			m.ChannelID,
			fmt.Sprintf("%s ни разу не пидор!", m.Author.Mention()),
		)
		if err != nil {
			return
		}
		return
	}

	pidors := sortPidors(stats)
	rating := 0
	for i, pidor := range pidors {
		if pidor.Name == v.Name {
			rating = i + 1
			break
		}
	}

	_, err = s.ChannelMessageSend(
		m.ChannelID,
		fmt.Sprintf("%s - пидор #%d, целых %d раз!", m.Author.Mention(), rating, v.Count),
	)
}

// pidorRow is a flattened, sortable view of one user's lifetime counter.
type pidorRow struct {
	Name  string
	Count int
}

// sortPidors orders the map by Count DESC, then by Name ASC for
// deterministic tie-breaks.
func sortPidors(stats map[string]repository.PidorStat) []pidorRow {
	rows := make([]pidorRow, 0, len(stats))
	for _, s := range stats {
		rows = append(rows, pidorRow{Name: s.Name, Count: s.Count})
	}
	slices.SortFunc(rows, func(a, b pidorRow) int {
		if a.Count != b.Count {
			return cmp.Compare(b.Count, a.Count)
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return rows
}

// sendPidorStatsRender is the shared renderer for the top-N view used
// by both !pidorstats and (indirectly) other commands.
func (b *Bot) sendPidorStatsRender(s *discordgo.Session, channelID string, stats map[string]repository.PidorStat) {
	pidors := sortPidors(stats)
	total := 0
	for _, p := range pidors {
		total += p.Count
	}
	if len(pidors) > pidorStatsLen {
		pidors = pidors[:pidorStatsLen]
	}

	var sb strings.Builder
	if len(pidors) == 0 {
		sb.WriteString("Все пидоры тут")
	} else {
		sb.WriteString(fmt.Sprintf("Крутили бота %d раз. Топ пидоров:\n", total))
		for _, pidor := range pidors {
			sb.WriteString(fmt.Sprintf("%s: %d\n", pidor.Name, pidor.Count))
		}
	}

	_, err := s.ChannelMessageSend(channelID, sb.String())
	if err != nil {
		return
	}
}
