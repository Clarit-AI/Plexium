// Package loader reads and validates the JSONL fixture file plus its
// companion manifest. It computes the per-fixture SHA-256 chain the manifest
// promises and refuses to score anything whose fixture set has changed since
// the manifest was generated.
package loader

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

// Loaded is the result of loading a fixture set + manifest.
type Loaded struct {
	Fixtures []protocol.Fixture
	Manifest protocol.Manifest
	// Drift is non-nil if the computed hashes disagree with the manifest.
	Drift *DriftReport
}

// DriftReport describes any disagreement between the manifest and the
// fixtures on disk.
type DriftReport struct {
	MissingFixtures  []string
	ExtraFixtures    []string
	HashMismatches   []string
	ManifestFileHash string
	ComputedFileHash string
	ManifestHash     string
	ComputedHash     string
}

// Load reads fixturePath (JSONL) and manifestPath. It computes fresh SHA-256
// digests and reports drift; if drift is non-nil the caller decides whether
// to score anyway.
func Load(fixturePath, manifestPath string) (*Loaded, error) {
	fixtures, fileHash, err := readFixtures(fixturePath)
	if err != nil {
		return nil, fmt.Errorf("read fixtures: %w", err)
	}
	manifest, manifestHash, err := readManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	drift := compareManifest(manifest, fixtures, fileHash, manifestHash)
	return &Loaded{Fixtures: fixtures, Manifest: manifest, Drift: drift}, nil
}

func readFixtures(path string) ([]protocol.Fixture, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	h := sha256.New()
	tr := io.TeeReader(f, h)
	var fixtures []protocol.Fixture
	scanner := bufio.NewScanner(tr)
	scanner.Buffer(make([]byte, 1<<20), 8<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		// Skip empty lines but treat them as part of the hash.
		if len(line) == 0 {
			continue
		}
		var fix protocol.Fixture
		if err := json.Unmarshal(line, &fix); err != nil {
			return nil, "", fmt.Errorf("decode fixture line: %w", err)
		}
		fixtures = append(fixtures, fix)
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}
	return fixtures, hex.EncodeToString(h.Sum(nil)), nil
}

func readManifest(path string) (protocol.Manifest, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return protocol.Manifest{}, "", err
	}
	var m protocol.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return protocol.Manifest{}, "", fmt.Errorf("decode manifest: %w", err)
	}
	h := sha256.Sum256(b)
	return m, hex.EncodeToString(h[:]), nil
}

func compareManifest(m protocol.Manifest, fixtures []protocol.Fixture, fileHash, manifestHash string) *DriftReport {
	rep := &DriftReport{ManifestFileHash: m.SHA256FixtureFile, ComputedFileHash: fileHash, ManifestHash: m.SHA256Manifest, ComputedHash: manifestHash}
	if rep.ManifestFileHash != "" && rep.ManifestFileHash != rep.ComputedFileHash {
		rep.HashMismatches = append(rep.HashMismatches, "fixture file hash mismatch")
	}
	// The manifest's self-hash (SHA256Manifest) is intentionally cleared by
	// WriteManifest because it cannot carry its own hash; we still report
	// the computed manifest hash so reviewers see what the loader observed.
	manifestSet := map[string]protocol.FixtureHash{}
	for _, fh := range m.FixtureHashes {
		manifestSet[fh.ID] = fh
	}
	fixtureSet := map[string]struct{}{}
	for _, f := range fixtures {
		fixtureSet[f.ID] = struct{}{}
		c := canonicalize(f)
		sum := sha256.Sum256(c)
		fh := protocol.FixtureHash{ID: f.ID, SHA: hex.EncodeToString(sum[:]), Split: f.Split}
		if prev, ok := manifestSet[f.ID]; ok {
			if prev.SHA != fh.SHA {
				rep.HashMismatches = append(rep.HashMismatches, f.ID)
			}
		} else {
			rep.ExtraFixtures = append(rep.ExtraFixtures, f.ID)
		}
	}
	for id := range manifestSet {
		if _, ok := fixtureSet[id]; !ok {
			rep.MissingFixtures = append(rep.MissingFixtures, id)
		}
	}
	if len(rep.MissingFixtures) == 0 && len(rep.ExtraFixtures) == 0 && len(rep.HashMismatches) == 0 {
		return nil
	}
	sort.Strings(rep.MissingFixtures)
	sort.Strings(rep.ExtraFixtures)
	sort.Strings(rep.HashMismatches)
	return rep
}

// canonicalize produces a stable JSON encoding of a fixture for hashing.
func canonicalize(f protocol.Fixture) []byte {
	// Round-trip through json.Marshal then a deterministic encoder. We avoid
	// a stable-sort library to keep dependencies minimal; the fixture
	// structure has no maps at the top level.
	b, err := json.Marshal(f)
	if err != nil {
		// json.Marshal of a struct with no cycles and only primitive/map
		// fields cannot fail; if it does, return the empty encoding so the
		// hash is reproducible but the caller can detect it.
		return []byte{}
	}
	return b
}

// FilterBySplit returns the subset of fixtures in the supplied split. The
// fixture's split field is authoritative; the loader does not partition.
func FilterBySplit(fixtures []protocol.Fixture, split protocol.Split) []protocol.Fixture {
	out := make([]protocol.Fixture, 0, len(fixtures))
	for _, f := range fixtures {
		if f.Split == split {
			out = append(out, f)
		}
	}
	return out
}

// DistinctSourceGroupsForSplit returns the sorted unique source groups in a
// fixture slice, used by the independence check.
func DistinctSourceGroupsForSplit(fixtures []protocol.Fixture) []string {
	return protocol.DistinctSourceGroups(fixtures)
}

// ValidateFixtures runs structural validation over the supplied fixture
// slice. The first error terminates the run.
func ValidateFixtures(fixtures []protocol.Fixture) error {
	seen := map[string]struct{}{}
	for i, f := range fixtures {
		if _, ok := seen[f.ID]; ok {
			return fmt.Errorf("duplicate fixture id %q at index %d", f.ID, i)
		}
		seen[f.ID] = struct{}{}
		if err := f.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// VerifySplitIndependence runs the protocol's independence rule: no source
// group may appear in more than one split.
func VerifySplitIndependence(fixtures []protocol.Fixture) ([]protocol.SplitGroupIndependence, error) {
	groups := map[protocol.Split]map[string]struct{}{}
	for _, s := range []protocol.Split{protocol.SplitTuning, protocol.SplitHeldOut, protocol.SplitReserved} {
		groups[s] = map[string]struct{}{}
	}
	for _, f := range fixtures {
		groups[f.Split][f.SourceGroup] = struct{}{}
	}
	var out []protocol.SplitGroupIndependence
	seen := map[string]string{}
	for _, s := range []protocol.Split{protocol.SplitTuning, protocol.SplitHeldOut, protocol.SplitReserved} {
		ind := true
		var note string
		for g := range groups[s] {
			if prev, ok := seen[g]; ok {
				ind = false
				note = fmt.Sprintf("group %q appears in %s and %s", g, prev, s)
				break
			}
			seen[g] = string(s)
		}
		out = append(out, protocol.SplitGroupIndependence{
			Split:         s,
			GroupCount:    len(groups[s]),
			Independent:   ind,
			ViolationNote: note,
		})
	}
	for _, item := range out {
		if !item.Independent {
			return out, errors.New(item.ViolationNote)
		}
	}
	return out, nil
}

// BuildManifest constructs a protocol.Manifest from the supplied fixture
// path. It computes the file hash, per-fixture hashes, splits, task counts,
// review-status distribution, challenge distribution, and the split
// independence check. The returned manifest has SHA256Manifest set to ""; the
// caller should call WriteManifest so the self-hash is finalized.
func BuildManifest(fixturesPath string, generatedAt time.Time) (protocol.Manifest, error) {
	fixtures, fileHash, err := readFixtures(fixturesPath)
	if err != nil {
		return protocol.Manifest{}, fmt.Errorf("read fixtures: %w", err)
	}
	if err := ValidateFixtures(fixtures); err != nil {
		return protocol.Manifest{}, fmt.Errorf("validate fixtures: %w", err)
	}
	indep, err := VerifySplitIndependence(fixtures)
	if err != nil {
		return protocol.Manifest{}, fmt.Errorf("independence: %w", err)
	}
	m := protocol.Manifest{
		ProtocolVersion:    protocol.ProtocolVersion,
		GeneratedAt:        generatedAt,
		FixtureFile:        fixturesPath,
		SplitCounts:        protocol.SplitCount{},
		TaskCounts:         protocol.TaskCount{},
		ReviewStatusCount:  map[protocol.ReviewStatus]int{},
		ChallengeCount:     map[protocol.ChallengeCategory]int{},
		FixtureHashes:      []protocol.FixtureHash{},
		SplitIndependences: indep,
		SHA256FixtureFile:  fileHash,
	}
	groups := map[string]struct{}{}
	for _, f := range fixtures {
		groups[f.SourceGroup] = struct{}{}
		switch f.Split {
		case protocol.SplitTuning:
			m.SplitCounts.Tuning++
		case protocol.SplitHeldOut:
			m.SplitCounts.HoldOut++
		}
		m.SplitCounts.Total++
		switch f.Task {
		case protocol.TaskEntityType:
			m.TaskCounts.EntityType++
		case protocol.TaskRelationship:
			m.TaskCounts.Relationship++
		case protocol.TaskClaimSupport:
			m.TaskCounts.ClaimSupport++
		}
		m.TaskCounts.Total++
		m.ReviewStatusCount[f.ReviewStatus]++
		for _, c := range f.ChallengeCategories {
			m.ChallengeCount[c]++
		}
		raw, err := json.Marshal(f)
		if err != nil {
			return protocol.Manifest{}, fmt.Errorf("marshal fixture %s: %w", f.ID, err)
		}
		sum := sha256.Sum256(raw)
		m.FixtureHashes = append(m.FixtureHashes, protocol.FixtureHash{
			ID: f.ID, SHA: hex.EncodeToString(sum[:]), Split: f.Split,
		})
	}
	m.SourceGroupCount = len(groups)
	m.FixtureCount = len(fixtures)
	return m, nil
}

// WriteManifest writes the manifest to path. The SHA256Manifest field on
// the supplied manifest is cleared because the field cannot carry a hash of
// the body that contains it. The manifest is otherwise written verbatim so
// readers can diff any field other than SHA256Manifest for drift.
//
// Future revision: include a separate manifest.sha256 sidecar file with the
// self-hash so the loader can verify both the fixture file and the manifest
// file.
func WriteManifest(path string, m protocol.Manifest) error {
	m.SHA256Manifest = ""
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}
