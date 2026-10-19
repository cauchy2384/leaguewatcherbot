// Doppler-aware resolution of DATABASE_URL for the migrator.
//
// Resolution order:
//
//  1. -db flag value (explicit operator override)
//  2. DATABASE_URL env var (e.g. injected by `doppler run --`)
//  3. DOPPLER_TOKEN env var → fetch DATABASE_URL from the Doppler API
//     for project "leaguewatcherbot", config "prd"
//  4. error
//
// The third path exists so that "go run tools/migrator/main.go up"
// works directly on an operator machine that has DOPPLER_TOKEN in
// the environment, without requiring the Doppler CLI to be
// installed. See ADR-010 for the original rationale of preferring
// the direct-DSN path.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/nikoksr/doppler-go"
	"github.com/nikoksr/doppler-go/secret"
)

// ErrNoDatabaseURL is returned when no resolution path yields a DSN.
var ErrNoDatabaseURL = errors.New("DATABASE_URL not resolvable: pass -db, " +
	"export DATABASE_URL, or export DOPPLER_TOKEN to fetch from Doppler")

// dopplerLookupFunc abstracts the Doppler SDK call so it can be stubbed
// in unit tests. The caller is responsible for serialising access to
// the package-global doppler.Key (migrator is single-shot, so this is
// not a concern in practice).
type dopplerLookupFunc func(ctx context.Context, token string) (string, error)

// databaseResolverOption is a functional option for newCompositeResolver.
type databaseResolverOption func(*databaseResolver)

// withEnvLookup replaces the default env lookup (os.LookupEnv) — used in tests.
func withEnvLookup(fn func(string) (string, bool)) databaseResolverOption {
	return func(r *databaseResolver) { r.envLookup = fn }
}

// withDopplerLookup replaces the default Doppler SDK call — used in tests.
func withDopplerLookup(fn dopplerLookupFunc) databaseResolverOption {
	return func(r *databaseResolver) { r.dopplerLookup = fn }
}

// databaseResolver composes the precedence rules. Zero value is unusable;
// construct via newDatabaseResolver.
type databaseResolver struct {
	flagVal       string
	envLookup     func(string) (string, bool)
	dopplerLookup dopplerLookupFunc
}

func newDatabaseResolver(flagVal string, opts ...databaseResolverOption) *databaseResolver {
	r := &databaseResolver{
		flagVal:       flagVal,
		envLookup:     defaultEnvLookup,
		dopplerLookup: defaultDopplerLookup,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Resolve returns the DATABASE_URL that should be used. See the file
// doc comment for resolution order.
func (r *databaseResolver) Resolve(ctx context.Context) (string, error) {
	if r.flagVal != "" {
		return r.flagVal, nil
	}
	if v, ok := r.envLookup("DATABASE_URL"); ok && v != "" {
		return v, nil
	}
	token, ok := r.envLookup("DOPPLER_TOKEN")
	if !ok || token == "" {
		return "", ErrNoDatabaseURL
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

func defaultEnvLookup(key string) (string, bool) {
	return os.LookupEnv(key)
}

// defaultDopplerLookup is the production implementation backed by the
// doppler-go SDK. It sets the package-global API key before listing
// secrets — safe in the migrator because the process is single-shot.
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
