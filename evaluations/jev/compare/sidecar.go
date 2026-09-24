// Package compare binds the three evaluation arms to one frozen tuning
// corpus and request inventory. It is offline-only and never reads gold on
// the live request path.
package compare

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"

	"github.com/Clarit-AI/Plexium/evaluations/jev/baseline"
	"github.com/Clarit-AI/Plexium/evaluations/jev/loader"
	"github.com/Clarit-AI/Plexium/evaluations/jev/pilot"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

const Version = "jev-three-arm-comparison-v1"

// FrozenManifestSHA256 is the root of trust for report->corpus provenance:
// the SHA-256 of the frozen, human-reviewed execution manifest
// (review-pilot/fixtures.manifest.json) whose digest is pinned at inventory
// freeze (pilot request inventory corpusReference.manifestSha256). A
// fabricated corpus that regenerates its own manifest can be fully
// self-consistent, but its manifest SHA can never match this frozen
// identity, so a fabricated consistent set cannot verify.
const FrozenManifestSHA256 = "30e1c11cfe966ac5985ebb521a831a743829eaa5c75117cdbc80d26b7cc04978"

// RateSemanticsUnreconciled is the decision-2 report statement: B1 rate /
// currency / fee semantics are UNRECONCILED and live contracts are therefore
// never truthfully verified.
const (
	RateSemanticsUnreconciled = "UNRECONCILED"
	JevOutputRateBasis        = "documented free Jev output (current rate card); explicit zero, not an observed token count"
	AccountingBasis           = "fixed authorization-accounting envelope; not a provider tariff or verified maximum charge"
	MaxOutputTokensBound      = "non-cost resource bound; exceeding it halts and never represents billing exposure"
)

// CorpusProvenance is embedded in every report by its producer: the corpus
// digests, the fixture identity set, and the frozen execution-manifest
// identity. Build admits a report only if this matches the frozen corpus
// recomputed from the actual files AND the manifest SHA matches the frozen
// manifest identity.
type CorpusProvenance struct {
	FixtureFileSHA     string   `json:"fixtureFileSha256"`
	ManifestSHA        string   `json:"manifestSha256"`
	InventorySHA       string   `json:"inventorySha256"`
	FixtureIdentities  []string `json:"fixtureIdentities"`
	FixtureIdentitySHA string   `json:"fixtureIdentitySha256"`
}

// BillingBasis is embedded in every report: decision-1/2 policy statements.
// RateOut for documented free Jev output is an explicit zero (not omitted);
// MaxOutputTokens is a non-cost resource bound; rate semantics are
// UNRECONCILED and liveContractsVerified is never truthfully true.
type BillingBasis struct {
	RateSemantics             string `json:"rateSemantics"`
	LiveContractsVerified     bool   `json:"liveContractsVerified"`
	JevRateOutPerMillionMicro int64  `json:"jevRateOutPerMillionMicrodollars"`
	JevOutputRateBasis        string `json:"jevOutputRateBasis"`
	MaxOutputTokensBound      string `json:"maxOutputTokensBound"`
	AccountingBasis           string `json:"accountingBasis"`
}

// BoundReport is a scoring.Report with its embedded provenance and billing
// basis ("producers embed corpus digests + the fixture identity set + the
// execution-manifest SHA in every report").
type BoundReport struct {
	scoring.Report
	CorpusProvenance CorpusProvenance `json:"corpusProvenance"`
	BillingBasis     BillingBasis     `json:"billingBasis"`
}

// DefaultBillingBasis returns the decision-1/2 binding producers embed.
func DefaultBillingBasis() BillingBasis {
	return BillingBasis{
		RateSemantics:             RateSemanticsUnreconciled,
		LiveContractsVerified:     false,
		JevRateOutPerMillionMicro: 0,
		JevOutputRateBasis:        JevOutputRateBasis,
		MaxOutputTokensBound:      MaxOutputTokensBound,
		AccountingBasis:           AccountingBasis,
	}
}

// BuildProvenance computes the provenance block a producer must embed in
// every report for the given frozen corpus files and request inventory.
func BuildProvenance(fixturesPath, manifestPath string, inventorySHA string) (CorpusProvenance, error) {
	fixturesBytes, err := os.ReadFile(fixturesPath)
	if err != nil {
		return CorpusProvenance{}, fmt.Errorf("compare: read fixtures: %w", err)
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return CorpusProvenance{}, fmt.Errorf("compare: read manifest: %w", err)
	}
	loaded, err := loader.Load(fixturesPath, manifestPath)
	if err != nil {
		return CorpusProvenance{}, fmt.Errorf("compare: load corpus: %w", err)
	}
	if loaded.Drift != nil {
		return CorpusProvenance{}, errors.New("compare: fixture corpus or manifest has drift")
	}
	identities := make([]string, 0, len(loaded.Fixtures))
	for _, f := range loaded.Fixtures {
		identities = append(identities, f.ID)
	}
	sort.Strings(identities)
	return CorpusProvenance{
		FixtureFileSHA:     hashBytes(fixturesBytes),
		ManifestSHA:        hashBytes(manifestBytes),
		InventorySHA:       inventorySHA,
		FixtureIdentities:  identities,
		FixtureIdentitySHA: hashBytes([]byte(joinStrings(identities))),
	}, nil
}

// ProvenanceFromFiles computes the provenance block from the corpus files and
// the frozen request inventory on disk (the inventory identity pinned at
// inventory freeze).
func ProvenanceFromFiles(fixturesPath, manifestPath, inventoryPath string) (CorpusProvenance, error) {
	inventoryBytes, err := os.ReadFile(inventoryPath)
	if err != nil {
		return CorpusProvenance{}, fmt.Errorf("compare: read inventory: %w", err)
	}
	var inv pilot.Inventory
	if err := json.Unmarshal(inventoryBytes, &inv); err != nil {
		return CorpusProvenance{}, fmt.Errorf("compare: parse inventory: %w", err)
	}
	return BuildProvenance(fixturesPath, manifestPath, inv.InventoryHash)
}

func joinStrings(values []string) string {
	out := ""
	for i, v := range values {
		if i > 0 {
			out += "\n"
		}
		out += v
	}
	return out
}

type Config struct {
	FixturesPath    string
	ManifestPath    string
	InventoryPath   string
	BaselineReport  string
	LivePilotReport string
}

type CorpusIdentity struct {
	ProtocolVersion string `json:"protocolVersion"`
	FixtureFileSHA  string `json:"fixtureFileSha256"`
	ManifestSHA     string `json:"manifestSha256"`
	FixtureCount    int    `json:"fixtureCount"`
	InventorySHA    string `json:"inventorySha256"`
}

type ReportIdentity struct {
	Arm             string         `json:"arm"`
	JSONPointer     string         `json:"jsonPointer"`
	Source          scoring.Source `json:"source"`
	ProtocolVersion string         `json:"protocolVersion"`
	FixtureCount    int            `json:"fixtureCount"`
	BaselineVersion string         `json:"baselineVersion,omitempty"`
	ContainerSHA    string         `json:"containerSha256"`
	ReportSHA       string         `json:"reportSha256"`
}

type Sidecar struct {
	Version      string                    `json:"version"`
	ComparisonID string                    `json:"comparisonId"`
	Corpus       CorpusIdentity            `json:"corpus"`
	Reports      map[string]ReportIdentity `json:"reports"`
}

func Build(cfg Config) (*Sidecar, error) {
	if cfg.FixturesPath == "" || cfg.ManifestPath == "" || cfg.InventoryPath == "" || cfg.BaselineReport == "" || cfg.LivePilotReport == "" {
		return nil, errors.New("compare: fixtures, manifest, inventory, baseline report, and live pilot report are required")
	}

	// Read and hash the actual corpus files to bind the sidecar to the
	// producer's file artifacts, not just the inventory's claimed hashes.
	fixturesBytes, err := os.ReadFile(cfg.FixturesPath)
	if err != nil {
		return nil, fmt.Errorf("compare: read fixtures: %w", err)
	}
	fixtureFileSHA := hashBytes(fixturesBytes)

	manifestBytes, err := os.ReadFile(cfg.ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("compare: read manifest: %w", err)
	}
	manifestSHA := hashBytes(manifestBytes)

	inventoryBytes, err := os.ReadFile(cfg.InventoryPath)
	if err != nil {
		return nil, fmt.Errorf("compare: read inventory: %w", err)
	}
	// Compute inventory hash the same way as pilot package: canonical JSON without InventoryHash field
	var invForHash pilot.Inventory
	if err := json.Unmarshal(inventoryBytes, &invForHash); err != nil {
		return nil, fmt.Errorf("compare: parse inventory for hashing: %w", err)
	}
	invForHash.InventoryHash = ""
	canonicalInventory, err := json.Marshal(invForHash)
	if err != nil {
		return nil, fmt.Errorf("compare: marshal canonical inventory: %w", err)
	}
	inventorySHA := hashBytes(canonicalInventory)

	loaded, err := loader.Load(cfg.FixturesPath, cfg.ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("compare: load corpus: %w", err)
	}
	if loaded.Drift != nil {
		return nil, errors.New("compare: fixture corpus or manifest has drift")
	}
	var inv pilot.Inventory
	if err := readJSON(cfg.InventoryPath, &inv); err != nil {
		return nil, err
	}
	if err := pilot.ValidateInventory(&inv); err != nil {
		return nil, err
	}
	if err := pilot.ValidateScoringCorpus(&inv, loaded, cfg.ManifestPath); err != nil {
		return nil, fmt.Errorf("compare: corpus/inventory mismatch: %w", err)
	}

	// Verify that the inventory's claimed corpus hashes match the actual files.
	// This ensures the inventory is consistent with the corpus files.
	if inv.Corpus.FixtureFileSHA != fixtureFileSHA {
		return nil, fmt.Errorf("compare: inventory fixtureFileSHA %s does not match actual fixtures file %s", inv.Corpus.FixtureFileSHA, fixtureFileSHA)
	}
	if inv.Corpus.ManifestSHA != manifestSHA {
		return nil, fmt.Errorf("compare: inventory manifestSHA %s does not match actual manifest file %s", inv.Corpus.ManifestSHA, manifestSHA)
	}
	if inv.InventoryHash != inventorySHA {
		return nil, fmt.Errorf("compare: inventory hash %s does not match actual inventory file %s", inv.InventoryHash, inventorySHA)
	}

	baselineBytes, err := os.ReadFile(cfg.BaselineReport)
	if err != nil {
		return nil, err
	}
	pilotBytes, err := os.ReadFile(cfg.LivePilotReport)
	if err != nil {
		return nil, err
	}
	var baselineEnvelope struct {
		FixtureFileSHA string          `json:"fixtureFileSha256"`
		ManifestSHA    string          `json:"manifestSha256"`
		Baseline       json.RawMessage `json:"baseline"`
	}
	if err := json.Unmarshal(baselineBytes, &baselineEnvelope); err != nil || len(baselineEnvelope.Baseline) == 0 {
		return nil, errors.New("compare: baseline report does not contain baseline")
	}
	var pilotEnvelope struct {
		FixtureFileSHA string                     `json:"fixtureFileSha256"`
		ManifestSHA    string                     `json:"manifestSha256"`
		Scores         map[string]json.RawMessage `json:"tuningOnlyScores"`
	}
	if err := json.Unmarshal(pilotBytes, &pilotEnvelope); err != nil {
		return nil, fmt.Errorf("compare: decode pilot report: %w", err)
	}
	expect := corpusExpectation{
		ProtocolVersion: inv.Protocol,
		FixtureCount:    inv.Corpus.FixtureCount,
		Corpus:          expectedProvenance(fixtureFileSHA, manifestSHA, inventorySHA, loaded),
	}
	if err := matchContainerClaims("baseline container", baselineEnvelope.FixtureFileSHA, baselineEnvelope.ManifestSHA, expect); err != nil {
		return nil, err
	}
	if err := matchContainerClaims("pilot container", pilotEnvelope.FixtureFileSHA, pilotEnvelope.ManifestSHA, expect); err != nil {
		return nil, err
	}
	reports := make(map[string]ReportIdentity, 3)
	reports["baseline"], err = bindReport("baseline", "/baseline", baselineEnvelope.Baseline, baselineBytes, scoring.SourceBaseline, expect)
	if err != nil {
		return nil, err
	}
	for _, arm := range []string{string(pilot.ArmJev), string(pilot.ArmNano)} {
		raw := pilotEnvelope.Scores[arm]
		if len(raw) == 0 {
			return nil, fmt.Errorf("compare: pilot report missing %s arm", arm)
		}
		reports[arm], err = bindReport(arm, "/tuningOnlyScores/"+arm, raw, pilotBytes, scoring.Source(arm), expect)
		if err != nil {
			return nil, err
		}
	}
	sidecar := &Sidecar{
		Version: Version,
		Corpus: CorpusIdentity{
			ProtocolVersion: inv.Protocol,
			FixtureFileSHA:  fixtureFileSHA,
			ManifestSHA:     manifestSHA,
			FixtureCount:    inv.Corpus.FixtureCount,
			InventorySHA:    inventorySHA,
		},
		Reports: reports,
	}
	sidecar.ComparisonID, err = digestWithoutID(sidecar)
	if err != nil {
		return nil, err
	}
	return sidecar, nil
}

func Verify(sidecar *Sidecar, cfg Config) error {
	if sidecar == nil || sidecar.Version != Version || sidecar.ComparisonID == "" {
		return errors.New("compare: invalid sidecar identity")
	}

	// Recompute corpus hashes from the actual files and verify they match
	// the sidecar's recorded CorpusIdentity. This prevents a caller from
	// presenting a sidecar built from one corpus as if it were built from another.
	fixturesBytes, err := os.ReadFile(cfg.FixturesPath)
	if err != nil {
		return fmt.Errorf("compare: read fixtures for verification: %w", err)
	}
	if hashBytes(fixturesBytes) != sidecar.Corpus.FixtureFileSHA {
		return errors.New("compare: fixtures file hash does not match sidecar corpus identity")
	}

	manifestBytes, err := os.ReadFile(cfg.ManifestPath)
	if err != nil {
		return fmt.Errorf("compare: read manifest for verification: %w", err)
	}
	if hashBytes(manifestBytes) != sidecar.Corpus.ManifestSHA {
		return errors.New("compare: manifest file hash does not match sidecar corpus identity")
	}

	inventoryBytes, err := os.ReadFile(cfg.InventoryPath)
	if err != nil {
		return fmt.Errorf("compare: read inventory for verification: %w", err)
	}
	// Compute inventory hash the same way as pilot package
	var invForHash pilot.Inventory
	if err := json.Unmarshal(inventoryBytes, &invForHash); err != nil {
		return fmt.Errorf("compare: parse inventory for verification: %w", err)
	}
	invForHash.InventoryHash = ""
	canonicalInventory, err := json.Marshal(invForHash)
	if err != nil {
		return fmt.Errorf("compare: marshal canonical inventory for verification: %w", err)
	}
	if hashBytes(canonicalInventory) != sidecar.Corpus.InventorySHA {
		return errors.New("compare: inventory file hash does not match sidecar corpus identity")
	}

	// Also verify the full sidecar matches a rebuild (which re-runs every
	// provenance, manifest-identity and BaselineVersion admission check).
	expected, err := Build(cfg)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(sidecar, expected) {
		return errors.New("compare: sidecar digest or report identity mismatch")
	}
	// Explicit BaselineVersion validation on verification (decision W3-R4):
	// the claimed v0.4 comparison must bind a known baseline version.
	for arm, id := range sidecar.Reports {
		if err := validateBaselineVersion(arm, id.BaselineVersion); err != nil {
			return err
		}
	}
	return nil
}

func validateBaselineVersion(arm, version string) error {
	if arm == "baseline" {
		if version != string(baseline.BaselineV2) && version != string(baseline.BaselineLegacy) {
			return fmt.Errorf("compare: baseline report baselineVersion %q is not a known baseline version", version)
		}
		return nil
	}
	if version != "" {
		return fmt.Errorf("compare: %s report must not claim a baseline version, got %q", arm, version)
	}
	return nil
}

func Marshal(sidecar *Sidecar) ([]byte, error) {
	if sidecar == nil {
		return nil, errors.New("compare: nil sidecar")
	}
	return json.MarshalIndent(sidecar, "", "  ")
}

type corpusExpectation struct {
	ProtocolVersion string
	FixtureCount    int
	Corpus          CorpusProvenance
}

// expectedProvenance recomputes the provenance identity from the corpus
// digests actually hashed from the files plus the loaded fixture identity
// set.
func expectedProvenance(fixtureFileSHA, manifestSHA, inventorySHA string, loaded *loader.Loaded) CorpusProvenance {
	identities := make([]string, 0, len(loaded.Fixtures))
	for _, f := range loaded.Fixtures {
		identities = append(identities, f.ID)
	}
	sort.Strings(identities)
	return CorpusProvenance{
		FixtureFileSHA:     fixtureFileSHA,
		ManifestSHA:        manifestSHA,
		InventorySHA:       inventorySHA,
		FixtureIdentities:  identities,
		FixtureIdentitySHA: hashBytes([]byte(joinStrings(identities))),
	}
}

// matchContainerClaims rejects a container whose (optional) top-level corpus
// digest claims contradict the frozen corpus. Contradicting claims are never
// admitted alongside embedded provenance.
func matchContainerClaims(container, fixtureFileSHA, manifestSHA string, expect corpusExpectation) error {
	if fixtureFileSHA != "" && fixtureFileSHA != expect.Corpus.FixtureFileSHA {
		return fmt.Errorf("compare: %s fixtureFileSha256 claim %s does not match the frozen corpus", container, fixtureFileSHA)
	}
	if manifestSHA != "" && manifestSHA != expect.Corpus.ManifestSHA {
		return fmt.Errorf("compare: %s manifestSha256 claim %s does not match the frozen manifest", container, manifestSHA)
	}
	return nil
}

func matchProvenance(arm string, got CorpusProvenance, expect corpusExpectation) error {
	if got.FixtureFileSHA == "" && got.ManifestSHA == "" && got.InventorySHA == "" && len(got.FixtureIdentities) == 0 {
		return fmt.Errorf("compare: %s report carries no embedded corpus provenance", arm)
	}
	if got.FixtureFileSHA != expect.Corpus.FixtureFileSHA || got.ManifestSHA != expect.Corpus.ManifestSHA || got.InventorySHA != expect.Corpus.InventorySHA {
		return fmt.Errorf("compare: %s report corpus digests do not match the frozen corpus recomputed from the actual files", arm)
	}
	if got.ManifestSHA != FrozenManifestSHA256 {
		return fmt.Errorf("compare: %s report manifest SHA %s does not match the frozen manifest identity", arm, got.ManifestSHA)
	}
	if len(got.FixtureIdentities) != len(expect.Corpus.FixtureIdentities) || got.FixtureIdentitySHA != expect.Corpus.FixtureIdentitySHA {
		return fmt.Errorf("compare: %s report fixture identity set does not match the frozen corpus", arm)
	}
	for i := range got.FixtureIdentities {
		if got.FixtureIdentities[i] != expect.Corpus.FixtureIdentities[i] {
			return fmt.Errorf("compare: %s report fixture identity set does not match the frozen corpus", arm)
		}
	}
	return nil
}

func matchBillingBasis(arm string, got BillingBasis) error {
	if got.RateSemantics != RateSemanticsUnreconciled {
		return fmt.Errorf("compare: %s report must state rate semantics %s, got %q", arm, RateSemanticsUnreconciled, got.RateSemantics)
	}
	if got.LiveContractsVerified {
		return fmt.Errorf("compare: %s report must not claim live contracts verified while rate semantics are unreconciled", arm)
	}
	return nil
}

func bindReport(arm, pointer string, raw, container []byte, wantSource scoring.Source, expect corpusExpectation) (ReportIdentity, error) {
	var report BoundReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return ReportIdentity{}, fmt.Errorf("compare: decode %s report: %w", arm, err)
	}
	if report.Source != wantSource || report.ProtocolVersion != expect.ProtocolVersion || report.FixtureCount != expect.FixtureCount {
		return ReportIdentity{}, fmt.Errorf("compare: %s report identity mismatch: source=%q protocol=%q fixtures=%d", arm, report.Source, report.ProtocolVersion, report.FixtureCount)
	}
	if err := matchProvenance(arm, report.CorpusProvenance, expect); err != nil {
		return ReportIdentity{}, err
	}
	if err := matchBillingBasis(arm, report.BillingBasis); err != nil {
		return ReportIdentity{}, err
	}
	if err := validateBaselineVersion(arm, report.BaselineVersion); err != nil {
		return ReportIdentity{}, err
	}
	var canonical any
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return ReportIdentity{}, err
	}
	canonicalBytes, err := json.Marshal(canonical)
	if err != nil {
		return ReportIdentity{}, err
	}
	return ReportIdentity{Arm: arm, JSONPointer: pointer, Source: report.Source, ProtocolVersion: report.ProtocolVersion, FixtureCount: report.FixtureCount, BaselineVersion: report.BaselineVersion, ContainerSHA: hash(container), ReportSHA: hash(canonicalBytes)}, nil
}

func digestWithoutID(sidecar *Sidecar) (string, error) {
	copy := *sidecar
	copy.ComparisonID = ""
	b, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	return hash(b), nil
}

func readJSON(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("compare: decode %s: %w", path, err)
	}
	return nil
}

func hash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
