package compare

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestW3R4_SidecarAcceptsForeignCorpusReports reproduces W3-R4:
// Build/Verify accept reports from a FOREIGN corpus as long as source/protocol/count match.
// The sidecar attaches the SUPPLIED corpus hashes (from inventory) without verifying
// they match the actual fixtures/manifest files. Reports are only checked for
// source/protocol/count, not fixture identity.
func TestW3R4_SidecarAcceptsForeignCorpusReports(t *testing.T) {
	dir := t.TempDir()

	// Create a legitimate baseline and pilot report from the REAL corpus,
	// stamped with the embedded provenance an honest producer emits.
	baselinePath := filepath.Join(dir, "baseline.json")
	pilotPath := filepath.Join(dir, "pilot.json")
	writeGenuineReports(t, baselinePath, pilotPath, nil)

	// Build a legitimate sidecar with REAL corpus
	realCfg := Config{
		FixturesPath:    "../review-pilot/fixtures.jsonl",
		ManifestPath:    "../review-pilot/fixtures.manifest.json",
		InventoryPath:   "../pilot/request-inventory.json",
		BaselineReport:  baselinePath,
		LivePilotReport: pilotPath,
	}

	realSidecar, err := Build(realCfg)
	if err != nil {
		t.Fatalf("Build real sidecar failed: %v", err)
	}

	// Verify real sidecar with real config - should pass
	if err := Verify(realSidecar, realCfg); err != nil {
		t.Fatalf("Real sidecar rejected with real config: %v", err)
	}
	t.Log("Real sidecar verified with real config: OK")

	// NOW THE ATTACK:
	// Attacker creates a FOREIGN corpus (different fixtures) but with same protocol/count
	foreignDir := t.TempDir()
	foreignFixtures := filepath.Join(foreignDir, "fixtures.jsonl")
	foreignManifest := filepath.Join(foreignDir, "fixtures.manifest.json")
	foreignInventory := filepath.Join(foreignDir, "request-inventory.json")

	// Create foreign fixtures/manifest that are internally consistent
	createForeignCorpus(t, foreignFixtures, foreignManifest)

	// Create a FAKE inventory that matches the foreign corpus hashes
	createFakeInventoryMatchingForeign(t, foreignInventory, foreignFixtures, foreignManifest)

	// Attacker builds a sidecar with:
	// - FOREIGN fixtures/manifest
	// - FAKE inventory (matching foreign corpus)
	// - REAL reports (from real corpus)
	// This should FAIL because the reports are from a different corpus
	foreignCfg := Config{
		FixturesPath:    foreignFixtures,
		ManifestPath:    foreignManifest,
		InventoryPath:   foreignInventory,
		BaselineReport:  baselinePath,
		LivePilotReport: pilotPath,
	}

	foreignSidecar, err := Build(foreignCfg)
	if err != nil {
		t.Logf("Foreign corpus rejected at Build: %v", err)
		return
	}

	// The sidecar was built with foreign corpus identity but real reports
	// Verify with the same foreign config should also fail
	err = Verify(foreignSidecar, foreignCfg)
	if err == nil {
		t.Fatal("W3-R4 REPRODUCED: Foreign corpus sidecar accepted - reports from real corpus accepted with foreign corpus identity. bindReport only checks source/protocol/count, not fixture identity.")
	}
	t.Logf("W3-R4 FIXED: Foreign corpus rejected: %v", err)

	// Also test: verify the REAL sidecar with FOREIGN config
	// This should fail because the corpus hashes don't match
	err = Verify(realSidecar, foreignCfg)
	if err == nil {
		t.Fatal("W3-R4 REPRODUCED: Real sidecar accepted with foreign config - Verify re-runs Build with foreign config and gets matching sidecar")
	}
	t.Logf("Real sidecar correctly rejected with foreign config: %v", err)
}

func writeTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func createForeignCorpus(t *testing.T, fixturesPath, manifestPath string) {
	t.Helper()
	// Create a minimal but different corpus (24 fixtures, different content)
	fixtures := []map[string]any{
		{"id": "foreign-001", "task": "entity-type", "sourceGroup": "sg-foreign-001", "split": "tuning", "expectedLabel": "place", "question": "Foreign Q1?", "excerpts": []map[string]any{{"text": "Foreign corpus fixture 1"}}, "allowedLabels": []string{"place", "project"}, "challengeCategories": []string{"straightforward-positive"}, "templateFamily": "foreign-family-1", "reviewStatus": "approved", "reviewer": "attacker", "author": "attacker", "rationale": "Foreign", "rationaleEvidence": "Foreign", "candidateSource": "foreign"},
	}
	for i := 1; i < 24; i++ {
		f := make(map[string]any)
		for k, v := range fixtures[0] {
			f[k] = v
		}
		idNum := i + 1
		f["id"] = "foreign-" + string(rune('0'+idNum/10)) + string(rune('0'+idNum%10))
		f["sourceGroup"] = "sg-foreign-" + string(rune('0'+idNum/10)) + string(rune('0'+idNum%10))
		fixtures = append(fixtures, f)
	}

	var data []byte
	for _, f := range fixtures {
		b, _ := json.Marshal(f)
		data = append(data, b...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(fixturesPath, data, 0600); err != nil {
		t.Fatalf("write foreign fixtures: %v", err)
	}

	// Create manifest for foreign corpus - compute correct hashes
	_ = hashBytes(data)
	manifest := map[string]any{
		"protocolVersion":      "0.4.0",
		"fixtureCount":         24,
		"sourceGroupCount":     24,
		"splitCounts":          map[string]int{"tuning": 24, "holdOut": 0},
		"taskCounts":           map[string]int{"entityType": 24, "candidateType": 0, "relationship": 0, "claimSupport": 0, "total": 24},
		"reviewStatusCount":    map[string]int{"approved": 24, "unreviewed": 0, "disputed": 0},
		"negativeTaskEvidence": []any{},
		"reviewReadiness":      map[string]any{"studyGateEligible": false, "unreviewedCount": 0, "approvedCount": 24, "gateIneligibleReason": "test"},
		"fixtureHashes":        []any{},
	}
	// Add fixture hashes to manifest
	for _, f := range fixtures {
		canonical, _ := json.Marshal(f)
		sum := sha256.Sum256(canonical)
		manifest["fixtureHashes"] = append(manifest["fixtureHashes"].([]any), map[string]any{
			"id":    f["id"],
			"sha":   hex.EncodeToString(sum[:]),
			"split": "tuning",
		})
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	manifestSHA := hashBytes(manifestBytes)
	manifest["manifestSHA"] = manifestSHA // Not used but for completeness

	b, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(manifestPath, append(b, '\n'), 0600); err != nil {
		t.Fatalf("write foreign manifest: %v", err)
	}
}

func createFakeInventoryMatchingForeign(t *testing.T, inventoryPath, fixturesPath, manifestPath string) {
	t.Helper()
	// Read the foreign fixtures/manifest to compute their hashes
	fixturesData, _ := os.ReadFile(fixturesPath)
	manifestData, _ := os.ReadFile(manifestPath)

	fixtureFileSHA := hashBytes(fixturesData)
	manifestSHA := hashBytes(manifestData)

	// Create inventory with foreign corpus hashes
	inventory := map[string]any{
		"protocol": "0.4.0",
		"corpus": map[string]any{
			"fixtureFileSHA": fixtureFileSHA,
			"manifestSHA":    manifestSHA,
			"fixtureCount":   24,
			"split":          "tuning",
		},
		"inventoryHash": "fake-inventory-hash",
		"entries":       []any{},
		"schedule":      []any{},
	}

	// Create minimal entries for 24 fixtures
	for i := 0; i < 24; i++ {
		id := "foreign-" + string(rune('0'+(i+1)/10)) + string(rune('0'+(i+1)%10))
		sg := "sg-foreign-" + string(rune('0'+(i+1)/10)) + string(rune('0'+(i+1)%10))

		entry := map[string]any{
			"input": map[string]any{
				"fixtureID":     id,
				"task":          "entity-type",
				"sourceGroup":   sg,
				"question":      "Foreign Q?",
				"excerpts":      []map[string]any{{"text": "Foreign"}},
				"allowedLabels": []string{"place"},
			},
			"payloads": []map[string]any{
				{"arm": "jev", "body": "{}", "sha256": "fake-sha", "inputHash": "fake-input-hash"},
				{"arm": "nano", "body": "{}", "sha256": "fake-sha", "inputHash": "fake-input-hash"},
			},
		}
		inventory["entries"] = append(inventory["entries"].([]any), entry)

		inventory["schedule"] = append(inventory["schedule"].([]any), map[string]any{
			"ordinal":    len(inventory["schedule"].([]any)) + 1,
			"fixtureID":  id,
			"arm":        "jev",
			"repetition": 1,
			"payloadSHA": "fake-sha",
		})
		inventory["schedule"] = append(inventory["schedule"].([]any), map[string]any{
			"ordinal":    len(inventory["schedule"].([]any)) + 1,
			"fixtureID":  id,
			"arm":        "nano",
			"repetition": 1,
			"payloadSHA": "fake-sha",
		})
	}

	// Compute inventory hash
	canonical, _ := json.Marshal(inventory)
	inventory["inventoryHash"] = hashBytes(canonical)

	b, _ := json.MarshalIndent(inventory, "", "  ")
	if err := os.WriteFile(inventoryPath, append(b, '\n'), 0600); err != nil {
		t.Fatalf("write fake inventory: %v", err)
	}
}
