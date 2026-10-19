package bot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"leaguewatcher/internal/leaguewatcher"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matryer/is"
)

// capturedTavilyRequest records what the fake Tavily server received.
type capturedTavilyRequest struct {
	authorization string
	contentType   string
	path          string
	body          tavilySearchRequest
}

// newFakeTavilyServer spins up an httptest server that mimics /search
// and records the last incoming request.
func newFakeTavilyServer(t *testing.T, status int, response any, captured *capturedTavilyRequest) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.path = r.URL.Path
		captured.authorization = r.Header.Get("Authorization")
		captured.contentType = r.Header.Get("Content-Type")

		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(raw, &captured.body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if response != nil {
			if err := json.NewEncoder(w).Encode(response); err != nil {
				t.Errorf("failed to encode fake response: %v", err)
			}
		}
	}))
}

func baseTavilyConfig(baseURL string) leaguewatcher.Config {
	return leaguewatcher.Config{
		TavilyAPIKey:         "test-tavily-key",
		TavilyBaseURL:        baseURL,
		TavilySearchDepth:    "basic",
		TavilyTopic:          "general",
		TavilyMaxResults:     3,
		TavilyTimeoutSeconds: 5,
	}
}

func TestSearchTavily_Success(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	captured := &capturedTavilyRequest{}
	server := newFakeTavilyServer(t, http.StatusOK, map[string]any{
		"results": []map[string]any{
			{"title": "Гайд по Ониксии", "url": "https://wow.example/onyxia", "content": "Ониксия — рейд на 40 человек, фаза 3.", "score": 0.95},
			{"title": "Гайд по Зул'Гурубу", "url": "https://wow.example/zg", "content": "Зул'Гуруб — рейд на 20 человек в Тернистой долине.", "score": 0.85},
		},
	}, captured)
	defer server.Close()

	cfg := baseTavilyConfig(server.URL)
	results, err := searchTavily(context.Background(), cfg, "гайд на Ониксию")

	is.NoErr(err)
	is.Equal(len(results), 2)
	is.Equal(results[0].Title, "Гайд по Ониксии")
	is.Equal(results[0].URL, "https://wow.example/onyxia")
	is.Equal(results[0].Content, "Ониксия — рейд на 40 человек, фаза 3.")
	is.Equal(results[0].Score, 0.95)

	// Verify request shape
	is.Equal(captured.path, "/search")
	is.Equal(captured.authorization, "Bearer test-tavily-key")
	is.Equal(captured.contentType, "application/json")
	is.Equal(captured.body.Query, "гайд на Ониксию")
	is.Equal(captured.body.SearchDepth, "basic")
	is.Equal(captured.body.Topic, "general")
	is.Equal(captured.body.MaxResults, 3)
	is.Equal(captured.body.IncludeAnswer, false)
	is.Equal(captured.body.IncludeRaw, false)
	is.Equal(captured.body.IncludeDomains, nil) // empty CSV → nil
	is.Equal(captured.body.ExcludeDomains, nil)
}

func TestSearchTavily_DomainFiltersFromCSV(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	captured := &capturedTavilyRequest{}
	server := newFakeTavilyServer(t, http.StatusOK, map[string]any{
		"results": []map[string]any{},
	}, captured)
	defer server.Close()

	cfg := baseTavilyConfig(server.URL)
	cfg.TavilyIncludeDomains = "project-ascension.fandom.com, ascension.gg ,"
	cfg.TavilyExcludeDomains = "pinterest.com , quora.com"

	_, err := searchTavily(context.Background(), cfg, "тактика Хаккар")
	is.NoErr(err)

	is.Equal(captured.body.IncludeDomains, []string{"project-ascension.fandom.com", "ascension.gg"})
	is.Equal(captured.body.ExcludeDomains, []string{"pinterest.com", "quora.com"})
}

func TestSearchTavily_CustomDepthAndMaxResults(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	captured := &capturedTavilyRequest{}
	server := newFakeTavilyServer(t, http.StatusOK, map[string]any{
		"results": []map[string]any{},
	}, captured)
	defer server.Close()

	cfg := baseTavilyConfig(server.URL)
	cfg.TavilySearchDepth = "advanced"
	cfg.TavilyMaxResults = 7
	cfg.TavilyTopic = "news"

	_, err := searchTavily(context.Background(), cfg, "рейд резет на Ониксии")
	is.NoErr(err)

	is.Equal(captured.body.SearchDepth, "advanced")
	is.Equal(captured.body.MaxResults, 7)
	is.Equal(captured.body.Topic, "news")
}

func TestSearchTavily_NotConfigured(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	cfg := leaguewatcher.Config{} // no API key
	_, err := searchTavily(context.Background(), cfg, "Сколько боссов в БВЛ?")

	is.True(err != nil)
	is.True(errors.Is(err, ErrTavilyNotConfigured))
}

func TestSearchTavily_Non2xx(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	captured := &capturedTavilyRequest{}
	server := newFakeTavilyServer(t, http.StatusTooManyRequests, map[string]any{
		"error": "rate limited",
	}, captured)
	defer server.Close()

	cfg := baseTavilyConfig(server.URL)
	_, err := searchTavily(context.Background(), cfg, "Хромаггус тактика")

	is.True(err != nil)
	is.True(!errors.Is(err, ErrTavilyNotConfigured)) // Transient, not config
	is.True(strings.Contains(err.Error(), "429"))
}

func TestSearchTavily_EmptyResults(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	captured := &capturedTavilyRequest{}
	server := newFakeTavilyServer(t, http.StatusOK, map[string]any{
		"results": []map[string]any{},
	}, captured)
	defer server.Close()

	cfg := baseTavilyConfig(server.URL)
	results, err := searchTavily(context.Background(), cfg, "Рагнарос бис шмот")

	is.NoErr(err)
	is.Equal(len(results), 0) // Empty is fine: caller proceeds LLM-only
}

func TestSearchTavily_InvalidJSON(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json{{{"))
	}))
	defer server.Close()

	cfg := baseTavilyConfig(server.URL)
	_, err := searchTavily(context.Background(), cfg, "Аттюн на Ониксию")

	is.True(err != nil)
	is.True(strings.Contains(err.Error(), "parse"))
}

func TestSearchTavily_Timeout(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block longer than the caller's context.
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	cfg := baseTavilyConfig(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := searchTavily(ctx, cfg, "Ониксия дыхание")

	is.True(err != nil)
	is.True(errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "deadline exceeded"))
}

func TestSplitCSV(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   ", nil},
		{"single", "a.com", []string{"a.com"}},
		{"multiple with spaces", "a.com, b.org ,c.net", []string{"a.com", "b.org", "c.net"}},
		{"trailing comma", "a.com,", []string{"a.com"}},
		{"all empty", ",,,", nil},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := is.New(t)
			is.Equal(splitCSV(tt.input), tt.want)
		})
	}
}
