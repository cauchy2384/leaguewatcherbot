package main

import (
	"context"
	"errors"
	"testing"

	"github.com/matryer/is"
)

func TestDatabaseResolver_Resolve(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("doppler api unreachable")

	testCases := []struct {
		name             string
		flagVal          string
		env              map[string]string
		dopplerResult    string
		dopplerErr       error
		wantURL          string
		wantErrContains  string
		wantErrIs        error
		wantDopplerCalls int
	}{
		{
			name:    "flag wins over env and Doppler",
			flagVal: "postgres://flag-dsn",
			env: map[string]string{
				"DATABASE_URL":  "postgres://env-dsn",
				"DOPPLER_TOKEN": "tok",
			},
			wantURL:          "postgres://flag-dsn",
			wantDopplerCalls: 0,
		},
		{
			name:    "env DATABASE_URL wins over Doppler",
			flagVal: "",
			env: map[string]string{
				"DATABASE_URL":  "postgres://env-dsn",
				"DOPPLER_TOKEN": "tok",
			},
			wantURL:          "postgres://env-dsn",
			wantDopplerCalls: 0,
		},
		{
			name:    "falls through to Doppler when env not set",
			flagVal: "",
			env: map[string]string{
				"DOPPLER_TOKEN": "tok",
			},
			dopplerResult:    "postgres://doppler-dsn",
			wantURL:          "postgres://doppler-dsn",
			wantDopplerCalls: 1,
		},
		{
			name:    "env DATABASE_URL empty string is treated as unset",
			flagVal: "",
			env: map[string]string{
				"DATABASE_URL":  "",
				"DOPPLER_TOKEN": "tok",
			},
			dopplerResult:    "postgres://doppler-dsn",
			wantURL:          "postgres://doppler-dsn",
			wantDopplerCalls: 1,
		},
		{
			name:    "Doppler returns error is wrapped",
			flagVal: "",
			env: map[string]string{
				"DOPPLER_TOKEN": "tok",
			},
			dopplerErr:       errBoom,
			wantErrContains:  "doppler lookup failed",
			wantDopplerCalls: 1,
		},
		{
			name:    "Doppler returns empty DATABASE_URL",
			flagVal: "",
			env: map[string]string{
				"DOPPLER_TOKEN": "tok",
			},
			dopplerResult:    "",
			wantErrContains:  "DATABASE_URL secret is missing or empty",
			wantDopplerCalls: 1,
		},
		{
			name:             "no resolution path available",
			flagVal:          "",
			env:              map[string]string{},
			wantErrIs:        ErrNoDatabaseURL,
			wantDopplerCalls: 0,
		},
		{
			name:    "DOPPLER_TOKEN set to empty string is treated as unset",
			flagVal: "",
			env: map[string]string{
				"DOPPLER_TOKEN": "",
			},
			wantErrIs:        ErrNoDatabaseURL,
			wantDopplerCalls: 0,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := is.New(t)

			dopplerCalls := 0
			stubDoppler := func(ctx context.Context, token string) (string, error) {
				dopplerCalls++
				is.Equal(token, "tok") // resolver must pass the token through
				return tt.dopplerResult, tt.dopplerErr
			}

			stubEnv := func(key string) (string, bool) {
				v, ok := tt.env[key]
				return v, ok
			}

			r := newDatabaseResolver(tt.flagVal,
				withEnvLookup(stubEnv),
				withDopplerLookup(stubDoppler))

			got, err := r.Resolve(context.Background())

			switch {
			case tt.wantErrIs != nil:
				is.True(errors.Is(err, tt.wantErrIs)) // errors.Is unwrap must work
			case tt.wantErrContains != "":
				is.True(err != nil) // expected error
				is.True(errContains(err.Error(), tt.wantErrContains))
			default:
				is.NoErr(err)
				is.Equal(got, tt.wantURL)
			}

			is.Equal(dopplerCalls, tt.wantDopplerCalls)
		})
	}
}

// errContains is a tiny strings.Contains that keeps test assertions readable
// without importing strings just for this.
func errContains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
