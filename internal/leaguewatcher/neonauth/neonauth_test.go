package neonauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matryer/is"
)

func TestCheckReachable(t *testing.T) {
	t.Parallel()

	t.Run("healthy service returns no error", func(t *testing.T) {
		t.Parallel()
		is := is.New(t)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			is.Equal(r.URL.Path, "/api/auth/ok") // must hit the well-known endpoint
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)

		is.NoErr(CheckReachable(context.Background(), srv.URL))
	})

	t.Run("trailing slash in base URL is handled", func(t *testing.T) {
		t.Parallel()
		is := is.New(t)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			is.Equal(r.URL.Path, "/api/auth/ok")
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)

		is.NoErr(CheckReachable(context.Background(), srv.URL+"/"))
	})

	t.Run("client error status is still reachable", func(t *testing.T) {
		t.Parallel()
		is := is.New(t)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(srv.Close)

		// A 4xx means the service responded; only 5xx is unhealthy.
		is.NoErr(CheckReachable(context.Background(), srv.URL))
	})

	t.Run("server error status returns error", func(t *testing.T) {
		t.Parallel()
		is := is.New(t)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		err := CheckReachable(context.Background(), srv.URL)
		is.True(err != nil)                                 // should error
		is.True(strings.Contains(err.Error(), "unhealthy")) // should mention unhealthy status
	})

	t.Run("empty base URL returns error", func(t *testing.T) {
		t.Parallel()
		is := is.New(t)

		err := CheckReachable(context.Background(), "")
		is.True(err != nil)                             // should error
		is.True(strings.Contains(err.Error(), "empty")) // should mention empty URL
	})

	t.Run("unreachable host returns error", func(t *testing.T) {
		t.Parallel()
		is := is.New(t)

		// Port 1 is reserved and refuses connections immediately.
		err := CheckReachable(context.Background(), "http://127.0.0.1:1")
		is.True(err != nil)                                   // should error
		is.True(strings.Contains(err.Error(), "unreachable")) // should mention unreachability
	})
}
