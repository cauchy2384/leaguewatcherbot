package db

import (
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// MigrationsFS embeds the SQL migration files so the binary carries its
// own schema definition. Source files live in
// internal/leaguewatcher/db/migrations/ and follow the
// {NNNN}_{snake_name}.{up|down}.sql convention.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Migration is a single versioned schema step.
type Migration struct {
	Version int
	Name    string
	UpSQL   string
	DownSQL string
}

// migrationNameRE matches `{NNNN}_{name}.{up|down}.sql`. Version is 1+
// digits (zero-padded to 4 in practice), name is lowercase snake_case
// (we allow digits but not uppercase, dashes, or extra dots in the name
// portion so discovery stays unambiguous).
var migrationNameRE = regexp.MustCompile(`^(\d+)_([a-z0-9_]+)\.(up|down)\.sql$`)

// LoadMigrations reads the embedded FS and returns the migrations sorted
// ascending by version. A migration must have both up and down files to
// be returned; an orphan file is a hard error (better to fail loudly at
// startup than silently skip half a schema change).
func LoadMigrations() ([]Migration, error) {
	entries, err := MigrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded migrations dir: %w", err)
	}

	type half struct {
		up   *string
		down *string
	}
	byVersion := map[int]*half{}
	names := map[int]string{}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationNameRE.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("embedded migration file %q does not match {NNNN}_{name}.{up|down}.sql", e.Name())
		}
		version, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("invalid version in %q: %w", e.Name(), err)
		}
		name := m[2]
		dir := m[3]

		raw, err := MigrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read %q: %w", e.Name(), err)
		}
		sql := string(raw)

		h, ok := byVersion[version]
		if !ok {
			h = &half{}
			byVersion[version] = h
		}
		if prev, exists := names[version]; exists && prev != name {
			return nil, fmt.Errorf("version %d has inconsistent names %q vs %q", version, prev, name)
		}
		names[version] = name

		switch dir {
		case "up":
			h.up = &sql
		case "down":
			h.down = &sql
		}
	}

	versions := make([]int, 0, len(byVersion))
	for v := range byVersion {
		versions = append(versions, v)
	}
	sort.Ints(versions)

	// Reject gaps: a missing version in the middle almost always means a
	// botched rebase of two parallel branches.
	for i, v := range versions {
		expected := i + 1
		if v != expected {
			return nil, fmt.Errorf("migration sequence has a gap: expected version %d, found %d", expected, v)
		}
	}

	out := make([]Migration, 0, len(versions))
	for _, v := range versions {
		h := byVersion[v]
		if h.up == nil {
			return nil, fmt.Errorf("version %d (%s) is missing its .up.sql file", v, names[v])
		}
		if h.down == nil {
			return nil, fmt.Errorf("version %d (%s) is missing its .down.sql file", v, names[v])
		}
		out = append(out, Migration{
			Version: v,
			Name:    names[v],
			UpSQL:   strings.TrimSpace(*h.up),
			DownSQL: strings.TrimSpace(*h.down),
		})
	}
	return out, nil
}
