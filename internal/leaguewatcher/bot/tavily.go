package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"leaguewatcher/internal/leaguewatcher"
	"net/http"
	"strings"
)

// ErrTavilyNotConfigured is returned when the Tavily Search integration is
// missing required configuration (API key). Callers should treat this as
// "feature disabled" rather than a transient failure.
var ErrTavilyNotConfigured = errors.New("tavily not configured")

// tavilyHTTPClient is shared across calls. *http.Client is safe for
// concurrent use; the per-call timeout is enforced via the request context.
var tavilyHTTPClient = &http.Client{}

// tavilySearchRequest is the subset of the Tavily Search API request we use.
// See https://docs.tavily.com/api-reference/endpoint/search.
type tavilySearchRequest struct {
	Query          string   `json:"query"`
	SearchDepth    string   `json:"search_depth,omitempty"`
	Topic          string   `json:"topic,omitempty"`
	MaxResults     int      `json:"max_results"`
	IncludeAnswer  bool     `json:"include_answer"`
	IncludeRaw     bool     `json:"include_raw_content"`
	IncludeDomains []string `json:"include_domains,omitempty"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
	TimeRange      string   `json:"time_range,omitempty"`
}

// tavilyResult is a single search hit.
type tavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// tavilySearchResponse is the subset of the Tavily response we consume.
type tavilySearchResponse struct {
	Results []tavilyResult `json:"results"`
}

// splitCSV splits a comma-separated value from Doppler into a slice,
// trimming whitespace and dropping empty entries. Returns nil for empty
// input so we omitempty at the JSON layer and let Tavily apply its own
// default behavior.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// searchTavily performs a single Tavily Search request for the query and
// returns the top results. Errors are non-fatal: callers should fall back
// to LLM-only behavior on any failure.
func searchTavily(ctx context.Context, cfg leaguewatcher.Config, query string) ([]tavilyResult, error) {
	if cfg.TavilyAPIKey == "" {
		return nil, ErrTavilyNotConfigured
	}

	reqBody := tavilySearchRequest{
		Query:          query,
		SearchDepth:    cfg.TavilySearchDepth,
		Topic:          cfg.TavilyTopic,
		MaxResults:     cfg.TavilyMaxResults,
		IncludeAnswer:  false, // the LLM synthesizes; we don't need Tavily's pre-cooked answer
		IncludeRaw:     false, // snippets only; raw HTML would blow up prompt size
		IncludeDomains: splitCSV(cfg.TavilyIncludeDomains),
		ExcludeDomains: splitCSV(cfg.TavilyExcludeDomains),
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := strings.TrimRight(cfg.TavilyBaseURL, "/") + "/search"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.TavilyAPIKey)

	resp, err := tavilyHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call tavily: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tavily returned %s: %s", resp.Status, truncateForLog(body))
	}

	var parsed tavilySearchResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(parsed.Results) == 0 {
		return nil, nil // Empty result set is not an error: caller proceeds LLM-only
	}

	return parsed.Results, nil
}
