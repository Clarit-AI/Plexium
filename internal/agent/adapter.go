package agent

import "context"

// CascadeLLMClient adapts a ProviderCascade to the lint.LLMClient interface.
// It bridges the agent package (context-aware, returns CompletionResult) with
// the lint package (simple string-in/string-out).
//
// This type satisfies lint.LLMClient structurally without importing the lint
// package, avoiding circular dependencies.
type CascadeLLMClient struct {
	Cascade *ProviderCascade
}

// Complete sends a prompt through the provider cascade and returns the response text.
func (c *CascadeLLMClient) Complete(prompt string) (string, error) {
	result, err := c.Cascade.Complete(context.Background(), prompt)
	if err != nil {
		return "", err
	}
	return result.Response, nil
}
