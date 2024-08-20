package bot

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
)

func (b *Bot) info(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {

	_, err := s.ChannelMessageSend(
		m.ChannelID,
		fmt.Sprintf(
			"%s, бот  не отвечает ни перед кем, кроме своих тёмных страстей и Богов Хаоса.\nСоздатель бота относится ко всем с глубочайшим уважением.",
			m.Author.Mention(),
		),
	)
	if err != nil {
		return
	}
}
