package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareWritesFrozenInventoryAndCorpusReference(t *testing.T) {
	d := t.TempDir()
	inv := filepath.Join(d, "inventory.json")
	ref := filepath.Join(d, "corpus.json")
	err := runCLI([]string{"prepare", "-fixtures", "../../review-pilot/fixtures.jsonl", "-manifest", "../../review-pilot/fixtures.manifest.json", "-out", inv, "-corpus-reference-out", ref, "-jev-request-model", "typesafe/jev-1.13", "-nano-request-model", "openai/gpt-4.1-nano", "-nano-provider", "OpenAI"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{inv, ref} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(`"expectedLabel"`)) {
			t.Fatalf("%s contains gold", p)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0222 != 0 {
			t.Fatalf("%s is writable: %v", p, info.Mode())
		}
	}
}

func TestRunFailsClosedWithoutReviewedContracts(t *testing.T) {
	p := filepath.Join(t.TempDir(), "execution.json")
	if err := os.WriteFile(p, []byte(`{"liveContractsVerified":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runCLI([]string{"run", "-execution-manifest", p}); err == nil {
		t.Fatal("run accepted unresolved live contracts")
	}
}
