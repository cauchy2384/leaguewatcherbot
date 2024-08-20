package repository

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

type Pidor struct {
	filename string

	Called map[string]time.Time
	Stats  map[string]map[string]PidorStat
}

type PidorStat struct {
	Name  string
	Count int
}

func NewPidor(filename string) (*Pidor, error) {
	if filename == "" {
		return nil, fmt.Errorf("pidors filename is empty")
	}
	fd, err := os.OpenFile(filename, os.O_RDONLY|os.O_CREATE, 0666)
	if err != nil {
		return nil, fmt.Errorf("%q open: %w", filename, err)
	}
	defer fd.Close()

	p := Pidor{
		filename: filename,
		Called:   make(map[string]time.Time),
		Stats:    make(map[string]map[string]PidorStat),
	}

	err = json.NewDecoder(fd).Decode(&p)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("%q decode: %w", filename, err)
	}

	return &p, nil
}

func (p *Pidor) Sync() error {

	fd, err := os.OpenFile(p.filename, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return fmt.Errorf("%q open: %w", p.filename, err)
	}
	defer fd.Close()

	enc := json.NewEncoder(fd)
	enc.SetIndent("", "\t")
	err = enc.Encode(p)
	if err != nil {
		return fmt.Errorf("%q encode: %w", p.filename, err)
	}

	return nil
}
