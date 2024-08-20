package bot

import (
	"context"
	"math/rand"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

func (b *Bot) khaleesi(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {

	input := m.Message.Content
	if utf8.RuneCountInString(input) < 10 {
		return
	}

	v := b.cnt.Add(1)
	if v < b.thresh.Load() {
		return
	}

	output, modified := b.kh.Modify(input)
	if !modified {
		return
	}

	_, err := s.ChannelMessageSendReply(m.ChannelID, output, m.Reference())
	if err != nil {
		return
	}

	b.resetKhaleesi()
}

func (b *Bot) resetKhaleesi() {
	b.cnt.Store(0)
	b.thresh.Store(rand.Int31()%20 + 10)
}
