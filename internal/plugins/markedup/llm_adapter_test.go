package markedup

import (
	"strings"
	"testing"
	"unicode/utf8"
)

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
