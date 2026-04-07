package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// DefaultOllamaHTTPPost
// ---------------------------------------------------------------------------

func TestDefaultOllamaHTTPPost_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"response":"Hello","eval_count":10,"done":true}`))
	}))
	defer srv.Close()

	resp, tokens, err := DefaultOllamaHTTPPost(context.Background(), srv.URL, `{"model":"test","prompt":"hi","stream":false}`)
	require.NoError(t, err)
	assert.Equal(t, "Hello", resp)
	assert.Equal(t, 10, tokens)
}

func TestDefaultOllamaHTTPPost_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer srv.Close()

	_, _, err := DefaultOllamaHTTPPost(context.Background(), srv.URL, `{}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestDefaultOllamaHTTPPost_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not valid json`))
	}))
	defer srv.Close()

	_, _, err := DefaultOllamaHTTPPost(context.Background(), srv.URL, `{}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing ollama response")
}

// ---------------------------------------------------------------------------
// DefaultOpenRouterHTTPPost
// ---------------------------------------------------------------------------

func TestDefaultOpenRouterHTTPPost_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Hello"}}],"usage":{"total_tokens":15}}`))
	}))
	defer srv.Close()

	resp, tokens, err := DefaultOpenRouterHTTPPost(context.Background(), srv.URL, `{}`, map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer sk-test",
	})
	require.NoError(t, err)
	assert.Equal(t, "Hello", resp)
	assert.Equal(t, 15, tokens)
}

func TestDefaultOpenRouterHTTPPost_HeadersSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer sk-test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Plexium/1.0", r.Header.Get("X-Title"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"total_tokens":5}}`))
	}))
	defer srv.Close()

	headers := map[string]string{
		"Authorization": "Bearer sk-test-key",
		"Content-Type":  "application/json",
		"X-Title":       "Plexium/1.0",
	}

	resp, tokens, err := DefaultOpenRouterHTTPPost(context.Background(), srv.URL, `{}`, headers)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Equal(t, 5, tokens)
}

func TestDefaultOpenRouterHTTPPost_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	_, _, err := DefaultOpenRouterHTTPPost(context.Background(), srv.URL, `{}`, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 429")
}

func TestDefaultOpenRouterHTTPPost_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{broken`))
	}))
	defer srv.Close()

	_, _, err := DefaultOpenRouterHTTPPost(context.Background(), srv.URL, `{}`, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing openrouter response")
}
