package markedup

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Clarit-AI/Plexium/internal/agent"
	"github.com/Clarit-AI/Plexium/internal/retry"
)

// nilCompletionProvider returns (nil, nil) from Complete — exercising
// the degenerate path where a provider swallows an error and yields no
// result. Used to pin the F1 (pass 5) nil-deref guard in
// CascadeLLMProvider.ExtractGraph.
type nilCompletionProvider struct{}

func (nilCompletionProvider) Name() string                                                  { return "nilcomp" }
func (nilCompletionProvider) IsAvailable() bool                                             { return true }
func (nilCompletionProvider) HealthCheck() error                                            { return nil }
func (nilCompletionProvider) CostPerToken() float64                                         { return 0 }
func (nilCompletionProvider) Complete(_ context.Context, _ string) (*agent.CompletionResult, error) {
	return nil, nil
}

// TestExtractGraph_NilCompletionDoesNotPanic is the regression test for
// CodeRabbit pass 5 F1: ProviderCascade.Complete may pass through a
// provider that returns (nil, nil); without the nil guard the adapter
// would panic on `completion.Response`. Assert we get a normal error
// containing "nil completion" so the enricher's non-fatal Tier 2 path
// can degrade to Tier 1.
func TestExtractGraph_NilCompletionDoesNotPanic(t *testing.T) {
	cascade := agent.NewCascade([]agent.Provider{nilCompletionProvider{}}, retry.DefaultPolicy())
	adapter := NewCascadeLLMProvider(cascade, nil)
	if adapter == nil {
		t.Fatalf("adapter must be non-nil with a usable cascade")
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ExtractGraph panicked on nil completion: %v", r)
		}
	}()

	res, summary, err := adapter.ExtractGraph(context.Background(), "# doc")
	if err == nil {
		t.Fatalf("expected error on nil completion, got result=%v summary=%q", res, summary)
	}
	if !strings.Contains(err.Error(), "nil completion") {
		t.Fatalf("expected error to mention 'nil completion', got: %v", err)
	}
	if res != nil || summary != "" {
		t.Fatalf("expected zero values on nil completion, got res=%v summary=%q", res, summary)
	}
}

// TestBuildExtractionPrompt_TruncationIsRuneSafe pins the rune-safe
// truncation invariant: when the body exceeds maxBody (8000 bytes) and
// the byte at position maxBody falls inside a multi-byte UTF-8 sequence,
// buildExtractionPrompt must back up to a rune boundary so the embedded
// document slice is valid UTF-8. A naive trimmed[:maxBody] would leave
// an invalid tail that some tokenizers reject.
func TestBuildExtractionPrompt_TruncationIsRuneSafe(t *testing.T) {
	// "é" is 2 bytes in UTF-8 (0xC3 0xA9). Place runs of "é" so the
	// 8000-byte cut lands mid-rune for at least one realistic input.
	// Pad with ASCII first so we control where the multi-byte sequence
	// straddles the cut point.
	const maxBody = 8000
	// 7999 ASCII bytes + a 2-byte rune → cut at index 8000 lands on
	// the second byte of the rune.
	body := strings.Repeat("a", maxBody-1) + "é" + strings.Repeat("b", 100)

	prompt := buildExtractionPrompt(body)

	if !utf8.ValidString(prompt) {
		t.Fatal("buildExtractionPrompt produced invalid UTF-8 — byte-level truncation likely split a rune")
	}

	// Belt-and-suspenders: extract the document body that was embedded
	// inside the prompt's ```md fence and confirm it too is valid UTF-8
	// AND does not contain the partial rune.
	start := strings.Index(prompt, "```md\n")
	end := strings.LastIndex(prompt, "\n```")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("could not locate document fence in prompt; got: %q", prompt)
	}
	doc := prompt[start+len("```md\n") : end]
	if !utf8.ValidString(doc) {
		t.Fatal("embedded document slice is not valid UTF-8")
	}
	if strings.ContainsRune(doc, utf8.RuneError) {
		t.Fatal("embedded document contains the UTF-8 replacement character (mid-rune cut)")
	}
}

// TestBuildExtractionPrompt_ShortBodyUnchanged guards the no-op path:
// inputs under maxBody must be embedded verbatim regardless of content.
func TestBuildExtractionPrompt_ShortBodyUnchanged(t *testing.T) {
	body := "短い日本語テキスト 🎉" // multi-byte but well under maxBody
	prompt := buildExtractionPrompt(body)
	if !strings.Contains(prompt, body) {
		t.Fatalf("short body must be embedded verbatim; missing from prompt")
	}
	if !utf8.ValidString(prompt) {
		t.Fatal("prompt is not valid UTF-8")
	}
}
