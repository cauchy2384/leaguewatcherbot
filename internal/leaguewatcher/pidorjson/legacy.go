// Package pidorjson contains the legacy pidors.json data shape.
//
// After the Postgres cutover (ADR-012), pidors.json is no longer a live
// store — the bot reads/writes only the `pidor_rolls` table. The file
// remains as an input format for one-shot tools (`tools/pidors-backfill`,
// `tools/pidors2csv`) that read existing pidors.json snapshots.
//
// Do not add I/O helpers here; this package is a pure data-shape
// definition. Tools that need to read pidors.json do their own decoding
// (see tools/pidors-backfill/main.go's loadPidors for the canonical
// pattern, tolerant of trailing garbage).
package pidorjson

import "time"

// Pidor mirrors the legacy pidors.json file format.
type Pidor struct {
	// Called records the most recent pidorday execution timestamp
	// per channel ID. Used to enforce the "one winner per channel
	// per day" rule before Postgres took over that duty.
	Called map[string]time.Time

	// Stats maps channel ID → user ID → lifetime counters. The
	// "lifetime" window is relative to whatever window of time the
	// pidors.json file was actively written by the live bot.
	Stats map[string]map[string]PidorStat
}

// PidorStat is the per-user lifetime aggregate within one channel.
type PidorStat struct {
	Name  string
	Count int
}
