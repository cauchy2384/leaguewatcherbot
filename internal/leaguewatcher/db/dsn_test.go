package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/matryer/is"
)

func TestDSNResolver_Resolve(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("doppler api unreachable")

	testCases := []struct {
		name             string
		explicit         string
		env              map[string]string
		dopplerResult    string
		dopplerErr       error
		wantURL          string
		wantErrContains  string
		wantErrIs        error
		wantDopplerCalls int
	}{
		{
			name:     "explicit wins over env and Doppler",
			explicit: "postgres://explicit-dsn",
			env: map[string]string{
				"DATABASE_URL":  "postgres://env-dsn",
				"DOPPLER_TOKEN": "tok",
			},
			wantURL:          "postgres://explicit-dsn",
			wantDopplerCalls: 0,
		},
		{
			name:     "env DATABASE_URL wins over Doppler",
			explicit: "",
			env: map[string]string{
				"DATABASE_URL":  "postgres://env-dsn",
				"DOPPLER_TOKEN": "tok",
			},
			wantURL:          "postgres://env-dsn",
			wantDopplerCalls: 0,
		},
		{
			name:     "falls through to Doppler when env not set",
			explicit: "",
			env: map[string]string{
				"DOPPLER_TOKEN": "tok",
			},
			dopplerResult:    "postgres://doppler-dsn",
			wantURL:          "postgres://doppler-dsn",
			wantDopplerCalls: 1,
		},
		{
			name:     "env DATABASE_URL empty string is treated as unset",
			explicit: "",
			env: map[string]string{
				"DATABASE_URL":  "",
				"DOPPLER_TOKEN": "tok",
			},
			dopplerResult:    "postgres://doppler-dsn",
			wantURL:          "postgres://doppler-dsn",
			wantDopplerCalls: 1,
		},
		{
			name:     "Doppler returns error is wrapped",
			explicit: "",
			env: map[string]string{
				"DOPPLER_TOKEN": "tok",
			},
			dopplerErr:       errBoom,
			wantErrContains:  "doppler lookup failed",
			wantDopplerCalls: 1,
		},
		{
			name:     "Doppler returns empty DATABASE_URL",
			explicit: "",
			env: map[string]string{
				"DOPPLER_TOKEN": "tok",
			},
			dopplerResult:    "",
			wantErrContains:  "DATABASE_URL secret is missing or empty",
			wantDopplerCalls: 1,
		},
		{
			name:             "no resolution path available",
			explicit:         "",
			env:              map[string]string{},
			wantErrIs:        ErrNoDSN,
			wantDopplerCalls: 0,
		},
		{
			name:     "DOPPLER_TOKEN set to empty string is treated as unset",
			explicit: "",
			env: map[string]string{
				"DOPPLER_TOKEN": "",
			},
			wantErrIs:        ErrNoDSN,
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

			r := NewDSNResolver(tt.explicit,
				WithEnvLookup(stubEnv),
				WithDopplerLookup(stubDoppler))

			got, err := r.Resolve(context.Background())

			switch {
			case tt.wantErrIs != nil:
				is.True(errors.Is(err, tt.wantErrIs)) // errors.Is unwrap must work
			case tt.wantErrContains != "":
				is.True(err != nil) // expected error
				is.True(strings.Contains(err.Error(), tt.wantErrContains))
			default:
				is.NoErr(err)
				is.Equal(got, tt.wantURL)
			}

			is.Equal(dopplerCalls, tt.wantDopplerCalls)
		})
	}
}
