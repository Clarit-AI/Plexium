package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// httpClient is the shared HTTP client for all provider transports.
var httpClient = &http.Client{Timeout: 120 * time.Second}

// ollamaResponse is the JSON structure returned by Ollama's /api/generate endpoint.
type ollamaResponse struct {
	Response  string `json:"response"`
	EvalCount int    `json:"eval_count"`
	Done      bool   `json:"done"`
}

// openRouterResponse is the OpenAI-compatible JSON structure returned by
// OpenRouter's /v1/chat/completions endpoint.
type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// DefaultOllamaHTTPPost performs a POST request against an Ollama API endpoint
// and returns the extracted response text and eval_count.
func DefaultOllamaHTTPPost(ctx context.Context, url, body string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed ollamaResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", 0, fmt.Errorf("parsing ollama response: %w", err)
	}

	return parsed.Response, parsed.EvalCount, nil
}

// DefaultOpenRouterHTTPPost performs a POST request against an OpenAI-compatible
// API endpoint (e.g. OpenRouter) and returns the extracted content and total token count.
func DefaultOpenRouterHTTPPost(ctx context.Context, url, body string, headers map[string]string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("creating request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("openrouter returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed openRouterResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", 0, fmt.Errorf("parsing openrouter response: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return "", 0, fmt.Errorf("openrouter response contains no choices")
	}

	return parsed.Choices[0].Message.Content, parsed.Usage.TotalTokens, nil
}
