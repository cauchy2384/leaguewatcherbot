package bot

import (
	"context"
	"fmt"
	"leaguewatcher/internal/leaguewatcher"
	"os"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/matryer/is"
	"go.uber.org/zap"
)

func TestBot(t *testing.T) {
	ctx := context.Background()
	is := is.New(t)

	token := os.Getenv("BOT_DISCORD_TOKEN")

	ch := make(chan leaguewatcher.Match)

	bot, err := New(Config{
		Token:   token,
		OwnerID: "cauchy2384",
	}, ch, zap.NewNop())
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

	dg, err := discordgo.New("Bot " + token)
	is.NoErr(err)

	rdy := make(chan struct{})
	defer close(rdy)

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
