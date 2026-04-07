package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Clarit-AI/Plexium/internal/retry"
)

func TestCascadeLLMClient_Complete(t *testing.T) {
	provider := NewOllamaProvider("http://localhost:11434", "test-model",
		func(ctx context.Context, url, body string) (string, int, error) {
			return "test response", 10, nil
		},
	)

	cascade := NewCascade([]Provider{provider}, retry.DefaultPolicy())
	client := &CascadeLLMClient{Cascade: cascade}

	result, err := client.Complete("hello")
	require.NoError(t, err)
	assert.Equal(t, "test response", result)
}

func TestCascadeLLMClient_Error(t *testing.T) {
	// Cascade with no available providers
	cascade := NewCascade([]Provider{}, retry.DefaultPolicy())
	client := &CascadeLLMClient{Cascade: cascade}

	_, err := client.Complete("hello")
	assert.Error(t, err)
}
