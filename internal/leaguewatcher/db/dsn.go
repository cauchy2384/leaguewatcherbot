// DSN (DATA SOURCE NAME) resolution for leaguewatcher CLI tools and
// auxiliaries that need a PostgreSQL connection string but are not the
// long-running Discord bot.
//
// Resolution order:
//
//  1. Explicit value passed by the caller (e.g. a -db flag)
//  2. DATABASE_URL env var (e.g. injected by `doppler run --`)
//  3. DOPPLER_TOKEN env var → fetch DATABASE_URL from the Doppler API
//     for project "leaguewatcherbot", config "prd"
//  4. error
//
// The third path exists so that single-shot tools can run on an operator
// machine that has DOPPLER_TOKEN in the environment, without requiring
// the Doppler CLI to be installed. See ADR-010 for the original
// rationale; ADR-011 §"Sharing the DSN resolver" for the move from
// tools/migrator to this package.
package db

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/nikoksr/doppler-go"
	"github.com/nikoksr/doppler-go/secret"
)

// ErrNoDSN is returned when no resolution path yields a DSN.
var ErrNoDSN = errors.New("database URL not resolvable: pass an explicit " +
	"value, export DATABASE_URL, or export DOPPLER_TOKEN to fetch from Doppler")

// DopplerLookupFunc abstracts the Doppler SDK call so it can be stubbed
// in unit tests. The caller is responsible for serialising access to the
// package-global doppler.Key — safe for single-shot tools, NOT safe for
// concurrent use within one process.
type DopplerLookupFunc func(ctx context.Context, token string) (string, error)

// DSNResolverOption is a functional option for NewDSNResolver.
type DSNResolverOption func(*DSNResolver)

// WithEnvLookup replaces the default env lookup (os.LookupEnv) — used in tests.
func WithEnvLookup(fn func(string) (string, bool)) DSNResolverOption {
	return func(r *DSNResolver) { r.envLookup = fn }
}

// WithDopplerLookup replaces the default Doppler SDK call — used in tests.
func WithDopplerLookup(fn DopplerLookupFunc) DSNResolverOption {
	return func(r *DSNResolver) { r.dopplerLookup = fn }
}

// DSNResolver composes the precedence rules. Zero value is unusable;
// construct via NewDSNResolver.
type DSNResolver struct {
	explicit      string
	envLookup     func(string) (string, bool)
	dopplerLookup DopplerLookupFunc
}

// NewDSNResolver builds a resolver with the given explicit override ("" if
// none) and any test-override options.
func NewDSNResolver(explicit string, opts ...DSNResolverOption) *DSNResolver {
	r := &DSNResolver{
		explicit:      explicit,
		envLookup:     os.LookupEnv,
		dopplerLookup: defaultDopplerLookup,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Resolve returns the database DSN that should be used. See the package
// doc comment for resolution order.
func (r *DSNResolver) Resolve(ctx context.Context) (string, error) {
	if r.explicit != "" {
		return r.explicit, nil
	}
	if v, ok := r.envLookup("DATABASE_URL"); ok && v != "" {
		return v, nil
	}
	token, ok := r.envLookup("DOPPLER_TOKEN")
	if !ok || token == "" {
		return "", ErrNoDSN
	}
	v, err := r.dopplerLookup(ctx, token)
	if err != nil {
		return "", fmt.Errorf("doppler lookup failed: %w", err)
	}
	if v == "" {
		return "", fmt.Errorf("DOPPLER_TOKEN is set but DATABASE_URL secret " +
			"is missing or empty in Doppler (project=leaguewatcherbot, config=prd)")
	}
	return v, nil
}

// defaultDopplerLookup is the production implementation backed by the
// doppler-go SDK. It sets the package-global API key before listing
// secrets and restores it on exit to keep test isolation when callers
// override via WithDopplerLookup.
func defaultDopplerLookup(ctx context.Context, token string) (string, error) {
	prevKey := doppler.Key
	doppler.Key = token
	defer func() { doppler.Key = prevKey }()

	secrets, _, err := secret.List(ctx, &doppler.SecretListOptions{
		Project: "leaguewatcherbot",
		Config:  "prd",
	})
	if err != nil {
		return "", err
	}
	s, ok := secrets["DATABASE_URL"]
	if !ok || s == nil || s.Computed == nil {
		return "", nil
	}
	return *s.Computed, nil
}
