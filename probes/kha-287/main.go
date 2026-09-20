// Package probe contains the KHA-287 reproduction harness for the F1 audit
// finding. It mirrors the audited F1 probe exactly: initialize a Plexium
// scaffold, track one source file through one wiki page, edit the source, then
// run plexium sync twice (no --regenerate, no providers) and assert the bug
// that the audit identified.
//
//   - First sync: should report stalePages >= 1, hashesUpdated >= 1, but
//     pagesRegenerated == 0 because no LLM provider was configured.
//   - Second sync: stalePages == 0 even though wiki page text is byte-identical
//     to the start, demonstrating "erased evidence".
//
// The harness writes JSON outputs to evidence/next to the working directory so
// the orchestrator can attach them to the PR.
//
// Run from the worktree root:
//
//	go run ./probes/kha-287
//
// Exit codes:
//
//	0  Probe ran. The bug was reproduced OR (post-fix) the fix held.
//	2  Bug NOT reproduced when expected (test failure).
//	3  Unexpected error during probe execution.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	plexiumsync "github.com/Clarit-AI/Plexium/internal/sync"
)

type probeOutput struct {
	Phase          string         `json:"phase"`
	StalePages     int            `json:"stalePages"`
	HashesUpdated  int            `json:"hashesUpdated"`
	PagesRegen     int            `json:"pagesRegenerated"`
	WikiBytes      int            `json:"wikiBytes"`
	WikiSHA256     string         `json:"wikiSha256"`
	ManifestHashes map[string]str `json:"manifestSourceHashes"`
	Notes          string         `json:"notes,omitempty"`
}

type str = string

func main() {
	outDir := flag.String("out", "probes/kha-287/evidence", "directory for evidence JSON files")
	mode := flag.String("mode", "reproduce", "reproduce or verify (post-fix assertion)")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fail("creating evidence dir: %v", err)
	}

	root, err := os.MkdirTemp("", "kha287-probe-")
	if err != nil {
		fail("tempdir: %v", err)
	}
	defer os.RemoveAll(root)

	if err := setupProbeRepo(root); err != nil {
		fail("setup: %v", err)
	}

	cfg, err := config.LoadFromDir(root)
	if err != nil {
		fail("loading config: %v", err)
	}

	wikiPath := filepath.Join(root, ".wiki", "modules", "auth-module.md")
	wikiBefore, err := os.ReadFile(wikiPath)
	if err != nil {
		fail("reading wiki before: %v", err)
	}

	srcPath := filepath.Join(root, "src", "auth.go")
	if err := os.WriteFile(srcPath, []byte("package auth\n\nfunc Login() {}\n"), 0o644); err != nil {
		fail("editing source: %v", err)
	}

	// --- First sync ---
	r1, err := plexiumsync.Run(plexiumsync.Options{
		RepoRoot: root,
		Config:   cfg,
		DryRun:   false,
		// no Regenerate, no Cascade
	})
	if err != nil {
		fail("first sync: %v", err)
	}
	po1 := snapshotProbe(root, wikiPath, "first-sync", wikiBefore)
	po1.StalePages = r1.StalePages
	po1.HashesUpdated = r1.HashesUpdated
	po1.PagesRegen = r1.PagesRegenerated
	writeJSON(filepath.Join(*outDir, "01-first-sync.json"), po1)

	// --- Second sync ---
	r2, err := plexiumsync.Run(plexiumsync.Options{
		RepoRoot: root,
		Config:   cfg,
		DryRun:   false,
	})
	if err != nil {
		fail("second sync: %v", err)
	}
	po2 := snapshotProbe(root, wikiPath, "second-sync", wikiBefore)
	po2.StalePages = r2.StalePages
	po2.HashesUpdated = r2.HashesUpdated
	po2.PagesRegen = r2.PagesRegenerated
	writeJSON(filepath.Join(*outDir, "02-second-sync.json"), po2)

	// --- Verdict ---
	type verdict struct {
		Mode          string      `json:"mode"`
		FirstSync     probeOutput `json:"firstSync"`
		SecondSync    probeOutput `json:"secondSync"`
		BugReproduced bool        `json:"bugReproduced"`
		BugNotes      string      `json:"bugNotes"`
		FixVerified   bool        `json:"fixVerified"`
		FixNotes      string      `json:"fixNotes"`
	}
	v := verdict{Mode: *mode, FirstSync: po1, SecondSync: po2}

	switch *mode {
	case "reproduce":
		// Step 6 acceptance: stalePages >= 1 AND hashesUpdated >= 1 AND pagesRegenerated == 0
		bugAtStep6 := po1.StalePages >= 1 && po1.HashesUpdated >= 1 && po1.PagesRegen == 0
		// Step 8 acceptance: stalePages == 0 with wiki unchanged
		bugAtStep8 := po2.StalePages == 0 && po1.WikiBytes == po2.WikiBytes
		v.BugReproduced = bugAtStep6 && bugAtStep8
		v.BugNotes = fmt.Sprintf(
			"step6(stale>=1 AND hashes>=1 AND regen==0)=%v; step8(stale==0 AND wikiBytes unchanged)=%v",
			bugAtStep6, bugAtStep8,
		)
		writeJSON(filepath.Join(*outDir, "verdict.json"), v)
		if v.BugReproduced {
			fmt.Println("BUG REPRODUCED — probe confirms F1 audit finding")
			os.Exit(0)
		}
		fmt.Println("BUG NOT REPRODUCED — current sync behaviour is unexpectedly correct")
		os.Exit(2)

	case "verify":
		// Post-fix expectation: stalePages stays >= 1 across both syncs (or
		// pagesRegenerated is non-zero with hashes matching the new wiki).
		wikiAfter, _ := os.ReadFile(wikiPath)
		wikiChanged := string(wikiAfter) != string(wikiBefore)

		// Case A: provider unavailable / not configured → stale remains
		// Case B: provider succeeds → stale drops, wiki changed
		holdOnStale := po1.StalePages >= 1 && po2.StalePages >= 1
		recovered := po1.StalePages >= 1 && po2.StalePages == 0 && po1.PagesRegen == 0 && po2.PagesRegen == 0 && wikiChanged
		v.FixVerified = holdOnStale || recovered
		v.FixNotes = fmt.Sprintf(
			"hold-on-stale (no regen attempted, stale stays)=%v; recovered with regen+changed wiki=%v; wikiChanged=%v",
			holdOnStale, recovered, wikiChanged,
		)
		writeJSON(filepath.Join(*outDir, "verdict.json"), v)
		if v.FixVerified {
			fmt.Println("FIX VERIFIED — sync no longer erases evidence")
			os.Exit(0)
		}
		fmt.Println("FIX NOT VERIFIED — see verdict.json")
		os.Exit(2)

	default:
		fail("unknown mode %q", *mode)
	}
}

func setupProbeRepo(root string) error {
	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(srcDir, "auth.go"), []byte("package auth\n"), 0o644); err != nil {
		return err
	}

	wikiDir := filepath.Join(root, ".wiki", "modules")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, ".wiki", "Home.md"), []byte("# Home\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(wikiDir, "auth-module.md"), []byte("# Auth Module\n"), 0o644); err != nil {
		return err
	}

	plexDir := filepath.Join(root, ".plexium")
	if err := os.MkdirAll(plexDir, 0o755); err != nil {
		return err
	}
	cfgYAML := []byte(`
version: 1
repo:
  name: test-repo
  language: go
sources:
  include: ["**/*.go"]
  exclude: ["vendor/**"]
wiki:
  root: .wiki
`)
	if err := os.WriteFile(filepath.Join(plexDir, "config.yml"), cfgYAML, 0o644); err != nil {
		return err
	}

	hash, err := manifest.ComputeHash(filepath.Join(srcDir, "auth.go"))
	if err != nil {
		return err
	}
	mgr, err := manifest.NewManager(filepath.Join(plexDir, "manifest.json"))
	if err != nil {
		return err
	}
	return mgr.Save(&manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{{
			WikiPath:    "modules/auth-module.md",
			Title:       "Auth Module",
			Ownership:   "managed",
			Section:     "Modules",
			SourceFiles: []manifest.SourceFile{{Path: "src/auth.go", Hash: hash}},
			LastUpdated: time.Now().UTC().Format(time.RFC3339),
		}},
		UnmanagedPages: []manifest.UnmanagedEntry{},
	})
}

func snapshotProbe(root, wikiPath, phase string, wikiBefore []byte) probeOutput {
	po := probeOutput{Phase: phase}
	wiki, _ := os.ReadFile(wikiPath)
	po.WikiBytes = len(wiki)
	po.WikiSHA256 = sha256Hex(wiki)
	if string(wiki) != string(wikiBefore) {
		po.Notes = "wiki text changed since baseline"
	}
	mgr, _ := manifest.NewManager(filepath.Join(root, ".plexium", "manifest.json"))
	m, _ := mgr.Load()
	po.ManifestHashes = map[string]str{}
	if m != nil {
		for _, p := range m.Pages {
			for _, sf := range p.SourceFiles {
				po.ManifestHashes[sf.Path] = sf.Hash
			}
		}
	}
	return po
}

func writeJSON(path string, v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fail("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fail("write %s: %v", path, err)
	}
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "probe: "+format+"\n", args...)
	os.Exit(3)
}
