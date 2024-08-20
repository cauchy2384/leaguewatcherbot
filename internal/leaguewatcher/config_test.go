package leaguewatcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConfig(t *testing.T) {
	fd, err := os.Open(filepath.Join("testdata", "config.yaml"))
	require.NoError(t, err)
	defer fd.Close()

	var cfg Config
	err = yaml.NewDecoder(fd).Decode(&cfg)
	require.NoError(t, err)
	t.Log(cfg)

	assert.NotEmpty(t, cfg.PollPeriod)
	assert.NotEmpty(t, cfg.PlayedGap)

	assert.NotNil(t, cfg.Players)
	for _, p := range cfg.Players {
		assert.NotEmpty(t, p.Name)
		assert.NotEmpty(t, p.Tag)
		assert.NotEmpty(t, p.Region)
		assert.NotEmpty(t, p.RealName)
	}

	assert.NoError(t, cfg.IsValid())
}
