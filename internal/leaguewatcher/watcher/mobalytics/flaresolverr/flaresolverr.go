// Package flaresolverr provides a client for FlareSolverr Docker container.
//
// FlareSolverr is a proxy server that solves Cloudflare challenges.
// It must be running as a separate Docker container (image: ghcr.io/flaresolverr/flaresolverr:latest).
//
// Usage:
//
//	// In docker-compose.yml, add:
//	services:
//	  flaresolverr:
//	    image: ghcr.io/flaresolverr/flaresolverr:latest
//	    restart: unless-stopped
//
//	// Set environment variable:
//	FLARESOLVERR_URL=http://flaresolverr:8191
//
//	// In main.go:
//	flare := flaresolverr.NewClient(logger)
//	client := mobalytics.NewClient(logger, flare)
package flaresolverr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

// CookieGetter implements mobalytics.CookieGetter via FlareSolverr.
type CookieGetter struct {
	baseURL string
	client  *http.Client
	logger  *slog.Logger

	mu        sync.RWMutex
	cookie    string
	expiry    time.Time
	solveOnce sync.Once
	stopChan  chan struct{} // closed to stop the background fetcher
}

// BaseURL returns the configured FlareSolverr URL.
func (c *CookieGetter) BaseURL() string {
	return c.baseURL
}

// NewClient creates a FlareSolverr cookie getter.
// FLARESOLVERR_URL env var: e.g. "http://flaresolverr:8191" or "http://localhost:8191"
func NewClient(logger *slog.Logger) *CookieGetter {
	cg := &CookieGetter{
		baseURL:  os.Getenv("FLARESOLVERR_URL") + "/v1",
		client:   &http.Client{Timeout: 30 * time.Second},
		logger:   logger,
		stopChan: make(chan struct{}),
	}
	// Start background fetcher that refreshes cookie every 14 minutes
	// This ensures cookie is always fresh and avoids first-request latency
	go cg.backgroundFetcher()
	return cg
}

// backgroundFetcher refreshes the cookie periodically (every 14 minutes).
// This keeps the cookie fresh without waiting for a 403 error.
func (c *CookieGetter) backgroundFetcher() {
	ticker := time.NewTicker(14 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			// Create context with timeout for this solve operation
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

			// Try to solve challenge in background (non-blocking)
			cookie, err := c.solve(ctx)
			cancel() // Always cancel to release resources

			if err != nil {
				if c.logger != nil {
					c.logger.Error("background flare solve failed", "error", err)
				}
				continue
			}
			c.mu.Lock()
			c.cookie = cookie
			c.expiry = time.Now().Add(14 * time.Minute)
			c.mu.Unlock()
			if c.logger != nil {
				c.logger.Info("background flare solved", "ttl", 14*time.Minute)
			}
		}
	}
}

// Stop stops the background fetcher.
// Should be called when the application is shutting down.
func (c *CookieGetter) Stop() {
	select {
	case <-c.stopChan:
		// Already closed
	default:
		close(c.stopChan)
	}
}

// GetCookie solves the Cloudflare challenge via FlareSolverr and returns cf_clearance.
// If the cached cookie is valid, it returns immediately.
// Otherwise it solves the challenge synchronously (blocking until solved or failed).
func (c *CookieGetter) GetCookie(ctx context.Context) string {
	// Return cached cookie if still valid
	c.mu.RLock()
	if c.cookie != "" && time.Now().Before(c.expiry) {
		c.mu.RUnlock()
		if c.logger != nil {
			c.logger.Debug("cf_clearance cached", "ttl", c.expiry.Sub(time.Now()).Truncate(time.Second))
		}
		return c.cookie
	}
	c.mu.RUnlock()

	// Solve challenge (once at a time)
	c.solveOnce.Do(func() {
		cookie, err := c.solve(ctx)
		if err != nil {
			if c.logger != nil {
				c.logger.Error("flare solve failed", "error", err)
			}
			return
		}
		c.mu.Lock()
		c.cookie = cookie
		c.expiry = time.Now().Add(14 * time.Minute)
		c.mu.Unlock()
		if c.logger != nil {
			c.logger.Info("flare solved", "ttl", 14*time.Minute)
		}
	})

	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cookie
}

// Reset invalidates the cached cookie so the next call will retry solving.
func (c *CookieGetter) Reset() {
	c.mu.Lock()
	c.cookie = ""
	c.mu.Unlock()
	c.solveOnce = sync.Once{}
}

type flareReq struct {
	Cmd        string `json:"cmd"`
	URL        string `json:"url"`
	MaxTimeout int    `json:"maxTimeout,omitempty"`
}

type flareCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
}

type flareSolution struct {
	Status  int           `json:"status"`
	Cookies []flareCookie `json:"cookies"`
	Message string        `json:"message"`
}

type flareResp struct {
	Status   string         `json:"status"`
	Message  string         `json:"message"`
	Solution *flareSolution `json:"solution"`
	Error    int            `json:"error,omitempty"`
}

func (c *CookieGetter) solve(ctx context.Context) (string, error) {
	if c.baseURL == "" {
		return "", fmt.Errorf("FLARESOLVERR_URL env var not set")
	}

	reqBody, err := json.Marshal(flareReq{
		Cmd:        "request.get",
		URL:        "https://app.mobalytics.gg/lol",
		MaxTimeout: 60000,
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http to %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d from %s: %s", resp.StatusCode, c.baseURL, string(body))
	}

	var r flareResp
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}

	if r.Error != 0 || r.Status != "ok" {
		return "", fmt.Errorf("solve failed: %s (code %d)", r.Message, r.Error)
	}
	if r.Solution == nil {
		return "", fmt.Errorf("no solution")
	}
	if r.Solution.Status != 200 {
		return "", fmt.Errorf("target status %d: %s", r.Solution.Status, r.Solution.Message)
	}

	for _, cookie := range r.Solution.Cookies {
		if cookie.Name == "cf_clearance" {
			return cookie.Value, nil
		}
	}

	return "", fmt.Errorf("cf_clearance not in cookies")
}
