package bot

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"leaguewatcher/internal/leaguewatcher"

	"github.com/bwmarrin/discordgo"
	"github.com/matryer/is"
)

func TestBot(t *testing.T) {
	ctx := t.Context()
	is := is.New(t)

	token := os.Getenv("BOT_DISCORD_TOKEN")
	if token == "" {
		t.Skip("BOT_DISCORD_TOKEN is not set")
	}

	ch := make(chan leaguewatcher.Match)

	bot, err := New(Config{
		Token:   token,
		OwnerID: "cauchy2384",
	}, ch, slog.New(slog.NewTextHandler(io.Discard, nil)))
	is.NoErr(err)

	_, err = bot.Run(ctx)
	is.NoErr(err)

	time.Sleep(10 * time.Second)
	ch <- leaguewatcher.Match{
		ID:        1,
		StartedAt: time.Now(),
		Player: leaguewatcher.Player{
			Name:   "name",
			Region: "region",
		},
		Win:     true,
		Kills:   100,
		Deaths:  0,
		Assists: 50,
	}

	ch <- leaguewatcher.Match{
		ID:        1,
		StartedAt: time.Now(),
		Player: leaguewatcher.Player{
			Name:   "name",
			Region: "region",
		},
		Win:     false,
		Kills:   100,
		Deaths:  0,
		Assists: 50,
	}

	time.Sleep(100 * time.Second)
}

func TestKek(t *testing.T) {
	is := is.New(t)

	token := os.Getenv("BOT_DISCORD_TOKEN")
	if token == "" {
		t.Skip("BOT_DISCORD_TOKEN is not set")
	}

	dg, err := discordgo.New("Bot " + token)
	is.NoErr(err)

	rdy := make(chan struct{})
	t.Cleanup(func() { close(rdy) })

	dg.AddHandler(func(s *discordgo.Session, event *discordgo.Ready) {
		rdy <- struct{}{}
	})

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		members, _ := s.GuildMembers("148124484803493888", "", 100)
		for _, m := range members {
			fmt.Println(m.User.Username, m.Nick, m.User.ID)
		}

		rdy <- struct{}{}
	})

	go dg.Open()
	<-rdy
	<-rdy
}
