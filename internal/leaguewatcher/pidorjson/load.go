package pidorjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Load reads a pidors.json file from disk.
//
// Matches the legacy bot's reader behaviour: parses the FIRST complete
// JSON document found in the file and silently ignores any trailing
// data. Real production pidors.json files have been observed with a
// few trailing characters from a previous bot-write bug; a strict
// json.Unmarshal would refuse them.
//
// Returns the parsed record. No warnings are surfaced for trailing data
// — callers that care (e.g., tools/pidors-backfill) use their own
// helper that surfaces them; this is the simple data-shape loader.
func Load(path string) (*Pidor, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var p Pidor
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &p, nil
}
