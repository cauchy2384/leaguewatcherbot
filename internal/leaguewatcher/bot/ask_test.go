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

	"github.com/matryer/is"
)

// capturedRequest records what the fake TOFA server received so tests can
// assert on the outgoing HTTP request.
type capturedRequest struct {
	authorization string
	contentType   string
	body          tofaChatRequest
}

// newFakeTOFAServer spins up an httptest server that mimics the
// OpenAI-compatible /chat/completions endpoint and records the last request.
func newFakeTOFAServer(t *testing.T, status int, response any, captured *capturedRequest) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}

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

func baseTestConfig(baseURL string) leaguewatcher.Config {
	return leaguewatcher.Config{
		TofaAPIKey:          "test-api-key",
		TofaModel:           "test-model",
		TofaBaseURL:         baseURL,
		TofaTemperature:     0.4,
		TofaTopP:            0.95,
		TofaMaxOutputTokens: 128,
	}
}

func TestGenerateWithTokenFactory_Success(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	captured := &capturedRequest{}
	server := newFakeTOFAServer(t, http.StatusOK, map[string]any{
		"choices": []map[string]any{
			{"message": map[string]string{"role": "assistant", "content": "Привет!"}},
		},
	}, captured)
	defer server.Close()

	cfg := baseTestConfig(server.URL)
	messages := buildMessages("hi", "", nil)
	answer, err := generateWithTokenFactory(context.Background(), cfg, messages)

	is.NoErr(err)
	is.Equal(answer, "Привет!")

	// Verify outgoing request shape
	is.Equal(captured.authorization, "Bearer test-api-key")
	is.Equal(captured.contentType, "application/json")
	is.Equal(captured.body.Model, "test-model")
	is.Equal(captured.body.Temperature, float32(0.4))
	is.Equal(captured.body.TopP, float32(0.95))
	is.Equal(captured.body.MaxTokens, int32(128))
	is.Equal(len(captured.body.Messages), 1)
	is.Equal(captured.body.Messages[0].Role, "user")
	is.Equal(captured.body.Messages[0].Content, "hi")
}

func TestGenerateWithTokenFactory_SystemPromptIncluded(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	captured := &capturedRequest{}
	server := newFakeTOFAServer(t, http.StatusOK, map[string]any{
		"choices": []map[string]any{
			{"message": map[string]string{"role": "assistant", "content": "ok"}},
		},
	}, captured)
	defer server.Close()

	cfg := baseTestConfig(server.URL)
	messages := buildMessages("hi", "Ты — бот.", nil)
	_, err := generateWithTokenFactory(context.Background(), cfg, messages)

	is.NoErr(err)
	is.Equal(len(captured.body.Messages), 2)
	is.Equal(captured.body.Messages[0].Role, "system")
	is.Equal(captured.body.Messages[0].Content, "Ты — бот.")
	is.Equal(captured.body.Messages[1].Role, "user")
	is.Equal(captured.body.Messages[1].Content, "hi")
}

func TestGenerateWithTokenFactory_SystemPromptOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	captured := &capturedRequest{}
	server := newFakeTOFAServer(t, http.StatusOK, map[string]any{
		"choices": []map[string]any{
			{"message": map[string]string{"role": "assistant", "content": "ok"}},
		},
	}, captured)
	defer server.Close()

	cfg := baseTestConfig(server.URL)
	messages := buildMessages("hi", "", nil)
	_, err := generateWithTokenFactory(context.Background(), cfg, messages)

	is.NoErr(err)
	is.Equal(len(captured.body.Messages), 1)
	is.Equal(captured.body.Messages[0].Role, "user")
}

func TestGenerateWithTokenFactory_NotConfigured(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cfg  leaguewatcher.Config
	}{
		{
			name: "missing api key",
			cfg: leaguewatcher.Config{
				TofaModel:   "m",
				TofaBaseURL: "https://example.invalid",
			},
		},
		{
			name: "missing model",
			cfg: leaguewatcher.Config{
				TofaAPIKey:  "k",
				TofaBaseURL: "https://example.invalid",
			},
		},
		{
			name: "missing both",
			cfg:  leaguewatcher.Config{},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := is.New(t)

			messages := buildMessages("hi", "", nil)
			_, err := generateWithTokenFactory(context.Background(), tt.cfg, messages)
			is.True(err != nil)
			is.True(errors.Is(err, ErrLLMNotConfigured))
		})
	}
}

func TestGenerateWithTokenFactory_Non2xx(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	captured := &capturedRequest{}
	server := newFakeTOFAServer(t, http.StatusTooManyRequests, map[string]any{
		"error": map[string]string{"message": "rate limited", "type": "rate_limit"},
	}, captured)
	defer server.Close()

	cfg := baseTestConfig(server.URL)
	messages := buildMessages("hi", "", nil)
	_, err := generateWithTokenFactory(context.Background(), cfg, messages)

	is.True(err != nil)
	is.True(!errors.Is(err, ErrLLMNotConfigured)) // Should be transient, not config
	is.True(strings.Contains(err.Error(), "429"))
}

func TestGenerateWithTokenFactory_EmptyChoices(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	captured := &capturedRequest{}
	server := newFakeTOFAServer(t, http.StatusOK, map[string]any{
		"choices": []map[string]any{},
	}, captured)
	defer server.Close()

	cfg := baseTestConfig(server.URL)
	messages := buildMessages("hi", "", nil)
	answer, err := generateWithTokenFactory(context.Background(), cfg, messages)

	is.NoErr(err)
	is.Equal(answer, "") // Bot.ask will translate this to "Оракул хранит молчание."
}

func TestBuildMessages_NoResults(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	msgs := buildMessages("what is ony", "base prompt", nil)

	is.Equal(len(msgs), 2)
	is.Equal(msgs[0].Role, "system")
	is.Equal(msgs[0].Content, "base prompt") // unchanged when no results
	is.Equal(msgs[1].Role, "user")
	is.Equal(msgs[1].Content, "what is ony")
}

func TestBuildMessages_NoResultsNoBasePrompt(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	msgs := buildMessages("what is ony", "", nil)

	is.Equal(len(msgs), 1) // no system message added
	is.Equal(msgs[0].Role, "user")
	is.Equal(msgs[0].Content, "what is ony")
}

func TestBuildMessages_SingleResultNoBasePrompt(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	results := []tavilyResult{
		{Title: "Onyxia Guide", URL: "https://example.com/ony", Content: "Onyxia is a 40-man raid boss."},
	}
	msgs := buildMessages("how many bosses", "", results)

	is.Equal(len(msgs), 2)
	is.Equal(msgs[0].Role, "system")
	// Should NOT start with the standard separator when base prompt was empty
	is.True(!strings.HasPrefix(msgs[0].Content, "\n\n"))
	is.True(strings.Contains(msgs[0].Content, "Контекст из поиска"))
	is.True(strings.Contains(msgs[0].Content, "Onyxia Guide"))
	is.True(strings.Contains(msgs[0].Content, "https://example.com/ony"))
	is.True(strings.Contains(msgs[0].Content, "Onyxia is a 40-man raid boss."))
}

func TestBuildMessages_MultipleResultsNumbered(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	results := []tavilyResult{
		{Title: "A", URL: "https://a.example", Content: "alpha"},
		{Title: "B", URL: "https://b.example", Content: "bravo"},
		{Title: "C", URL: "https://c.example", Content: "charlie"},
	}
	msgs := buildMessages("q", "base", results)

	is.Equal(len(msgs), 2)
	sys := msgs[0].Content
	is.True(strings.HasPrefix(sys, "base"))
	is.True(strings.Contains(sys, "1. A"))
	is.True(strings.Contains(sys, "2. B"))
	is.True(strings.Contains(sys, "3. C"))
	// User message must be untouched
	is.Equal(msgs[1].Role, "user")
	is.Equal(msgs[1].Content, "q")
}

func TestGenerateWithTokenFactory_InvalidJSON(t *testing.T) {
	t.Parallel()
	is := is.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json{{{"))
	}))
	defer server.Close()

	cfg := baseTestConfig(server.URL)
	messages := buildMessages("hi", "", nil)
	_, err := generateWithTokenFactory(context.Background(), cfg, messages)

	is.True(err != nil)
	is.True(strings.Contains(err.Error(), "parse"))
}
