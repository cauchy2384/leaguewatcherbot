// Package neonauth provides a lightweight reachability check for the Neon Auth
// service (Better Auth-based managed auth). Neon Auth is a TypeScript-only
// framework and cannot be embedded in this Go service; this package only
// verifies the service is reachable at startup. Full auth flows are deferred
// until a consumer exists (see ADR-009).
package neonauth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// checkTimeout bounds the reachability check (per ADR-009).
const checkTimeout = 5 * time.Second

// wellKnownPath is the Better Auth well-known health endpoint.
const wellKnownPath = "/api/auth/ok"

// CheckReachable performs a lightweight GET against the Neon Auth well-known
// endpoint ({baseURL}/api/auth/ok) to verify the service is reachable. Any
// HTTP response below 500 is treated as reachable; network failures and 5xx
// responses return an error. The baseURL is never logged by this function.
func CheckReachable(ctx context.Context, baseURL string) error {
	if baseURL == "" {
		return fmt.Errorf("neon auth base URL is empty")
	}

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	url := strings.TrimSuffix(baseURL, "/") + wellKnownPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to build neon auth check request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("neon auth unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("neon auth unhealthy: status %d", resp.StatusCode)
	}

	return nil
}
