package leaguewatcher

import (
	"fmt"
	"time"
)

type Config struct {
	PollPeriod time.Duration `yaml:"poll_period"`
	PlayedGap  time.Duration `yaml:"played_gap"`

	Players   []Player `yaml:"players"`
	ChannelID string   `yaml:"channel_id"`
}

func (cfg Config) IsValid() error {
	if cfg.PollPeriod <= 0 {
		return fmt.Errorf("poll_period must be positive")
	}
	if cfg.PlayedGap <= 0 {
		return fmt.Errorf("played_gap must be positive")
	}

	if len(cfg.Players) == 0 {
		return fmt.Errorf("players must not be empty")
	}

	for i, p := range cfg.Players {
		if p.Name == "" {
			return fmt.Errorf("player %d name must not be empty", i)
		}
		if p.Region == "" {
			return fmt.Errorf("player %d region must not be empty", i)
		}
		if p.RealName == "" {
			return fmt.Errorf("player %d real name must not be empty", i)
		}
	}

	if cfg.ChannelID == "" {
		return fmt.Errorf("channel ID must not be empty")
	}
	return nil
}
