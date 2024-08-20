package bot

import (
	"context"
	"fmt"
	"leaguewatcher/internal/leaguewatcher/bot/repository"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/kyokomi/emoji/v2"
	"go.uber.org/zap"
)

const (
	pidorStatsLen = 10
)

func (b *Bot) pidor(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {

	_, err := s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s, ты пидор!", m.Author.Mention()))
	if err != nil {
		return
	}
}

type Pidors struct {
	mu    sync.Mutex
	store *repository.Pidor
}

func NewPidors(filename string) (*Pidors, error) {

	pidors := Pidors{
		mu: sync.Mutex{},
	}

	var err error
	pidors.store, err = repository.NewPidor(filename)
	if err != nil {
		return nil, err
	}

	return &pidors, nil
}

func (p *Pidors) syncStore() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.store.Sync()
}

func (p *Pidors) WasChosen(cID string, pidor *discordgo.Member) {
	defer p.syncStore()

	p.mu.Lock()
	defer p.mu.Unlock()

	p.store.Called[cID] = time.Now()

	stats, ok := p.store.Stats[cID]
	if !ok {
		stats = make(map[string]repository.PidorStat)
	}

	id := pidor.User.ID

	s := stats[id]

	s.Name = pidor.User.Username
	s.Count = s.Count + 1

	stats[id] = s

	p.store.Stats[cID] = stats
}

func (p *Pidors) CanBeChosen(cID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	t, ok := p.store.Called[cID]
	if !ok {
		return true
	}

	return t.YearDay() != time.Now().YearDay()
}

func (p *Pidors) Stats(cID string) map[string]repository.PidorStat {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.store.Stats[cID]
}

func (b *Bot) pidorOfTheDay(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {

	cID := m.ChannelID

	if !b.pidors.CanBeChosen(cID) {
		_, err := s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s, сегодня уже выбирали, пидрила!", m.Author.Mention()))
		if err != nil {
			return
		}
		return
	}

	c, err := s.State.Channel(cID)
	if err != nil {
		b.logger.Warn("failed to get channel", zap.String("channel", cID), zap.Error(err))
		return
	}
	gID := c.GuildID
	members, err := s.GuildMembers(gID, "0", 1000)
	if err != nil {
		b.logger.Warn("failed to get guild members", zap.String("guild", gID), zap.Error(err))
		return
	}

	var pidors []*discordgo.Member
	for _, m := range members {
		if m.User.Bot {
			continue
		}
		pidors = append(pidors, m)
	}

	randomIndex := rand.Intn(len(pidors))
	pidor := pidors[randomIndex]
	b.logger.Debug("pidor of the day", zap.Any("pidor", pidor))

	_, err = s.ChannelMessageSend(c.ID,
		fmt.Sprintf(
			"Пидор дня сегодня... %s! %s",
			pidor.Mention(),
			emoji.Sprintf(":rooster:"),
		))
	if err != nil {
		return
	}

	b.pidors.WasChosen(cID, pidor)
}

func (b *Bot) pidorStats(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {

	stats := b.pidors.Stats(m.ChannelID)

	type Pidor struct {
		Name  string
		Count int
	}
	pidors := make([]Pidor, 0, len(stats))
	for _, s := range stats {
		pidors = append(pidors, Pidor{
			Name:  s.Name,
			Count: s.Count,
		})
	}
	sort.Slice(pidors, func(i, j int) bool {
		if pidors[i].Count != pidors[j].Count {
			return pidors[i].Count > pidors[j].Count
		}

		return pidors[i].Name < pidors[j].Name
	})

	total := 0
	for i := range pidors {
		total += pidors[i].Count
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

	_, err := s.ChannelMessageSend(m.ChannelID, sb.String())
	if err != nil {
		return
	}
}

func (b *Bot) pidorPersonalStats(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {
	stats := b.pidors.Stats(m.ChannelID)

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

	type Pidor struct {
		Name  string
		Count int
	}
	pidors := make([]Pidor, 0, len(stats))
	for _, s := range stats {
		pidors = append(pidors, Pidor{
			Name:  s.Name,
			Count: s.Count,
		})
	}
	sort.Slice(pidors, func(i, j int) bool {
		if pidors[i].Count != pidors[j].Count {
			return pidors[i].Count > pidors[j].Count
		}

		return pidors[i].Name < pidors[j].Name
	})

	rating := 0
	for i, pidor := range pidors {
		if pidor.Name == v.Name {
			rating = i + 1
			break
		}
	}

	_, err := s.ChannelMessageSend(
		m.ChannelID,
		fmt.Sprintf("%s - пидор #%d, целых %d раз!", m.Author.Mention(), rating, v.Count),
	)
	if err != nil {
		return
	}

}
