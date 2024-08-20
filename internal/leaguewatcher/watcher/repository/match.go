package repository

import (
	"fmt"
	"sync"
)

type Match struct {
	mu sync.Mutex
	m  map[string]int
}

func NewMatch() *Match {
	return &Match{
		m: make(map[string]int),
	}
}

func (m *Match) Set(region, summoner string, id int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.key(region, summoner)
	m.m[key] = id
}

func (m *Match) Get(region, summoner string) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.key(region, summoner)
	id, ok := m.m[key]
	return id, ok
}

func (m *Match) key(region, summoner string) string {
	return fmt.Sprintf("%s:%s", region, summoner)
}
