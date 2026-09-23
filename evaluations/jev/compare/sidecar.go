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

	"github.com/Clarit-AI/Plexium/evaluations/jev/loader"
	"github.com/Clarit-AI/Plexium/evaluations/jev/pilot"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

const Version = "jev-three-arm-comparison-v1"

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
	baselineBytes, err := os.ReadFile(cfg.BaselineReport)
	if err != nil {
		return nil, err
	}
	pilotBytes, err := os.ReadFile(cfg.LivePilotReport)
	if err != nil {
		return nil, err
	}
	var baselineEnvelope struct {
		Baseline json.RawMessage `json:"baseline"`
	}
	if err := json.Unmarshal(baselineBytes, &baselineEnvelope); err != nil || len(baselineEnvelope.Baseline) == 0 {
		return nil, errors.New("compare: baseline report does not contain baseline")
	}
	var pilotEnvelope struct {
		Scores map[string]json.RawMessage `json:"tuningOnlyScores"`
	}
	if err := json.Unmarshal(pilotBytes, &pilotEnvelope); err != nil {
		return nil, fmt.Errorf("compare: decode pilot report: %w", err)
	}
	reports := make(map[string]ReportIdentity, 3)
	reports["baseline"], err = bindReport("baseline", "/baseline", baselineEnvelope.Baseline, baselineBytes, scoring.SourceBaseline, inv.Protocol, inv.Corpus.FixtureCount)
	if err != nil {
		return nil, err
	}
	for _, arm := range []string{string(pilot.ArmJev), string(pilot.ArmNano)} {
		raw := pilotEnvelope.Scores[arm]
		if len(raw) == 0 {
			return nil, fmt.Errorf("compare: pilot report missing %s arm", arm)
		}
		reports[arm], err = bindReport(arm, "/tuningOnlyScores/"+arm, raw, pilotBytes, scoring.Source(arm), inv.Protocol, inv.Corpus.FixtureCount)
		if err != nil {
			return nil, err
		}
	}
	sidecar := &Sidecar{
		Version: Version,
		Corpus:  CorpusIdentity{ProtocolVersion: inv.Protocol, FixtureFileSHA: inv.Corpus.FixtureFileSHA, ManifestSHA: inv.Corpus.ManifestSHA, FixtureCount: inv.Corpus.FixtureCount, InventorySHA: inv.InventoryHash},
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
	expected, err := Build(cfg)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(sidecar, expected) {
		return errors.New("compare: sidecar digest or report identity mismatch")
	}
	return nil
}

func Marshal(sidecar *Sidecar) ([]byte, error) {
	if sidecar == nil {
		return nil, errors.New("compare: nil sidecar")
	}
	return json.MarshalIndent(sidecar, "", "  ")
}

func bindReport(arm, pointer string, raw, container []byte, wantSource scoring.Source, protocolVersion string, fixtureCount int) (ReportIdentity, error) {
	var report scoring.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		return ReportIdentity{}, fmt.Errorf("compare: decode %s report: %w", arm, err)
	}
	if report.Source != wantSource || report.ProtocolVersion != protocolVersion || report.FixtureCount != fixtureCount {
		return ReportIdentity{}, fmt.Errorf("compare: %s report identity mismatch: source=%q protocol=%q fixtures=%d", arm, report.Source, report.ProtocolVersion, report.FixtureCount)
	}
	var canonical any
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return ReportIdentity{}, err
	}
	canonicalBytes, err := json.Marshal(canonical)
	if err != nil {
		return ReportIdentity{}, err
	}
	return ReportIdentity{Arm: arm, JSONPointer: pointer, Source: report.Source, ProtocolVersion: report.ProtocolVersion, FixtureCount: report.FixtureCount, ContainerSHA: hash(container), ReportSHA: hash(canonicalBytes)}, nil
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
