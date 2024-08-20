package repository

import (
	"encoding/json"
	"fmt"
	"leaguewatcher/internal/leaguewatcher"
	"os"
	"sync"
)

type Log struct {
	filename string
	mu       sync.Mutex
}

func NewLog(filename string) *Log {
	return &Log{
		filename: filename,
		mu:       sync.Mutex{},
	}
}

func (l *Log) AddEvent(event leaguewatcher.Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	fd, err := os.OpenFile(l.filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("%q open: %w", l.filename, err)
	}
	defer fd.Close()

	data, err := json.Marshal(&event)
	if err != nil {
		return fmt.Errorf("%q marshal: %w", event, err)
	}

	data = append(data, '\n')

	_, err = fd.Write(data)
	if err != nil {
		return fmt.Errorf("%q write: %w", l.filename, err)
	}

	return nil
}
