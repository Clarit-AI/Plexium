// Package protocol defines the frozen data types used by the Jev offline
// evaluation harness: fixture schema, candidate generation rules, vocabularies,
// scoring, transport validation, and run artifacts.
//
// All identifiers and shapes in this file follow KHA-579 protocol v0.1. Any
// future change MUST bump the protocol version and produce a new manifest; no
// held-out result may inform tuning under the same version.
package protocol

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ProtocolVersion identifies the frozen state of the offline protocol.
// v0.4 supersedes v0.3 (evaluation-only, NOT production):
//   - Both typing tasks (entity-type, candidate-type) gain explicit
//     insufficient-evidence in the closed vocabulary, distinct from
//     transport failure. The legacy default-fallback vocabularies
//     remain unchanged so smoke 332 fixtures under v0.3 stay valid.
//   - Document-typing convention: type the document by its PRIMARY
//     SUBJECT (the most prominent thing it is about). If mixed
//     subjects have no clear primary, the gold is
//     insufficient-evidence (explicit abstention), NOT a baseline
//     fallback. A baseline that always returns a label is no longer
//     admissible as evidence-grounded gold for missing-evidence or
//     mixed-subject cases.
//   - Predicate directionality (resolved v0.4):
//     `used-by` reads "source is used by target" (target consumes
//     source). Reversing the edge source↔target direction inverts
//     whether used-by fits.
//   - Candidate-typing convention unchanged: homogeneous canonical
//     role vocabulary distinct from document-type vocabulary.
//   - Entity-name / alias disjointness across splits preserved.
//   - Calibration split (Choice confidence vs max-prob) preserved.
//   - Existing smoke 332 fixtures (v0.3) remain valid: their
//     AllowedLabels do not include insufficient-evidence, but
//     fixture-supplied AllowedLabels are accepted verbatim by the
//     validator, and protocol_version mismatches between fixtures
//     and manifest are surfaced via Drift. v0.3 manifests are
//     marked stale; no measured-accuracy claim is made against the
//     new abstention label without v0.4 fixtures that use it.
const ProtocolVersion = "0.4.0"

// SourceCommit records the inspected source revision for a fixture or
// baseline. An empty value indicates a synthetic source group that does not
// pin a real upstream commit.
type SourceCommit struct {
	Repository string `json:"repository"`
	Revision   string `json:"revision"`
	Note       string `json:"note,omitempty"`
}

// Task identifies one of the four classification tasks the harness scores.
type Task string

const (
	TaskEntityType    Task = "entity-type"    // document-level typing
	TaskCandidateType Task = "candidate-type" // semantic type of a single candidate entity
	TaskRelationship  Task = "relationship"   // directed edge predicate classification
	TaskClaimSupport  Task = "claim-support"  // supported / contradicted / insufficient
)

func (t Task) Valid() bool {
	switch t {
	case TaskEntityType, TaskCandidateType, TaskRelationship, TaskClaimSupport:
		return true
	}
	return false
}

// DocumentTypeLabels is the canonical whitelist for document-level typing,
// kept separate from CandidateTypeLabels (which describe a single extracted
// entity) and from MarkedUp's `Entity.Role` (the per-entity role assigned
// inside NER). The document classification is a property of the page, not
// of an extracted entity.
//
// v0.4 convention (user-selected 2026-09-20): type the document by its
// PRIMARY SUBJECT. If mixed subjects have no clear primary (e.g. a
// construction log mixing crew, place, and project content equally), the
// gold is insufficient-evidence (explicit abstention), NOT a baseline
// fallback. The legacy "first label = document" default fallback is no
// longer admissible as evidence-grounded gold for missing-evidence or
// mixed-subject cases.
//
// The first value ("document") remains the legacy deterministic-fallback
// baseline output; v0.4 baseline versions abstain instead (see
// baseline.PredictV2).
var DocumentTypeLabels = []string{
	"document",
	"person",
	"project",
	"concept",
	"organization",
	"software",
	"event",
	"place",
	"tool",
	"paper",
	"insufficient-evidence",
}

// CandidateTypeLabels is the homogeneous canonical vocabulary for the
// candidate-typing task. Labels describe the semantic type of an
// extracted entity (PERSON, ORGANIZATION, CONCEPT, etc) and are kept
// distinct from DocumentTypeLabels so the scorer does not mix two
// disjoint label spaces under one task.
//
// v0.4 convention (user-selected 2026-09-20): missing-evidence cases
// (no candidate name, no surrounding context) abstain as
// insufficient-evidence, NOT a baseline fallback. The legacy "first
// label = PERSON" default fallback is no longer admissible as
// evidence-grounded gold.
var CandidateTypeLabels = []string{
	"PERSON",
	"ORGANIZATION",
	"CONCEPT",
	"TOOL",
	"EVENT",
	"LOCATION",
	"DOCUMENT",
	"insufficient-evidence",
}

// PredicateLabels is the seven generic relationship predicates plus the two
// abstain-class labels from protocol v0.1: no-supported-relationship and
// insufficient-evidence. Per the spec, the abstain labels are explicit
// vocabulary entries; "Negated edges are unsupported; absent evidence is
// insufficient; conflicting equally authoritative evidence is insufficient
// unless fixture provenance establishes precedence."
//
// v0.4 directionality resolution: `used-by` reads "source is used by
// target" (i.e. the target consumes the source). Reversing edge
// source↔target inverts whether used-by fits. Other predicates follow
// their natural-language meaning; the protocol does not impose a fixed
// direction bit beyond this resolver for used-by.
var PredicateLabels = []string{
	"related-to",
	"derived-from",
	"implements",
	"depends-on",
	"created-by",
	"part-of",
	"used-by",
	"no-supported-relationship",
	"insufficient-evidence",
}

// VerdictLabels is the closed set of claim-support verdicts.
var VerdictLabels = []string{
	"supported",
	"contradicted",
	"insufficient-evidence",
}

// AllowedLabelsFor returns the protocol vocabulary for a task. Callers must
// score an unsupported label as an error.
func AllowedLabelsFor(t Task) []string {
	switch t {
	case TaskEntityType:
		return append([]string{}, DocumentTypeLabels...)
	case TaskCandidateType:
		return append([]string{}, CandidateTypeLabels...)
	case TaskRelationship:
		return append([]string{}, PredicateLabels...)
	case TaskClaimSupport:
		return append([]string{}, VerdictLabels...)
	}
	return nil
}

// IsAllowedLabel reports whether label is in the closed vocabulary for t.
func IsAllowedLabel(t Task, label string) bool {
	for _, v := range AllowedLabelsFor(t) {
		if v == label {
			return true
		}
	}
	return false
}

// Split is the held-out partition assignment.
type Split string

const (
	SplitTuning   Split = "tuning"
	SplitHeldOut  Split = "held-out"
	SplitReserved Split = "reserved"
)

// ReviewStatus is the human-review state of a fixture label. The harness
// refuses to score agent-authored labels as if they were human-reviewed; the
// only legitimate values for "reviewed" are Approved or Adjusted with a
// Reviewer handle.
type ReviewStatus string

const (
	ReviewUnreviewed ReviewStatus = "unreviewed"
	ReviewApproved   ReviewStatus = "approved"
	ReviewAdjusted   ReviewStatus = "adjusted"
	ReviewDisputed   ReviewStatus = "disputed"
	ReviewAbstain    ReviewStatus = "abstain"
)

// EvidenceSpan identifies a substring of an evidence passage by inclusive
// character offsets. The harness uses these to compute deterministic
// numeric/date invariants and to score supporting-span presence.
type EvidenceSpan struct {
	ExcerptID string `json:"excerptId"`
	StartChar int    `json:"startChar"`
	EndChar   int    `json:"endChar"`
	Text      string `json:"text"`
}

// Candidate is a shortlist entry passed to a classifier. Candidate IDs are
// stable per source-group and include the role tag used by the deterministic
// ranker (title, alias, wikilink, unknown).
type Candidate struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Alias string `json:"alias,omitempty"`
	Role  string `json:"role"` // title | alias | wikilink | unknown
}

// Excerpt is a passage of evidence text associated with a fixture case. The
// harness never reuses an excerpt across cases that are intended to be
// independent.
type Excerpt struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Revision string `json:"revision,omitempty"`
}

// NumericInvariant describes a numeric fact the harness expects the
// classifier to honor. It is recorded as evidence rationale and as a
// deterministic cross-check.
type NumericInvariant struct {
	ExcerptID string `json:"excerptId"`
	Span      string `json:"span"`
	Value     string `json:"value"`
	Unit      string `json:"unit,omitempty"`
}

// ChallengeCategory tags the perturbation or adversarial layer applied to a
// case. A case may carry multiple categories; the harness reports per-task
// breakdowns by each.
type ChallengeCategory string

const (
	ChallengeStraightPositive ChallengeCategory = "straightforward-positive"
	ChallengeStraightNegative ChallengeCategory = "straightforward-negative"
	ChallengeMissingEvidence  ChallengeCategory = "missing-evidence"
	ChallengeCompeting        ChallengeCategory = "competing-candidates"
	ChallengeNegation         ChallengeCategory = "negation"
	ChallengeReversedDir      ChallengeCategory = "reversed-direction"
	ChallengeRenamed          ChallengeCategory = "rename"
	ChallengeConflicting      ChallengeCategory = "conflicting-sources"
	ChallengeMisleading       ChallengeCategory = "misleading-numbers-dates"
	ChallengeIrrelevant       ChallengeCategory = "irrelevant-context"
	ChallengeHarmlessEdit     ChallengeCategory = "harmless-source-edit"
	ChallengeInvalidating     ChallengeCategory = "invalidating-edit"
	ChallengeAdversarial      ChallengeCategory = "adversarial-instruction"
)

// Fixture is the human-authored evaluation case. All author fields are
// populated even when the author is an agent; the Reviewer/ReviewStatus pair
// is what separates agent proposals from adjudicated labels.
//
// TemplateFamily identifies a perturbation template family (e.g. an
// adversarial-injection suffix string). The independence check refuses to
// place fixtures that share a TemplateFamily in different splits; the
// corpus generator must keep each family entirely within a single split.
//
// EdgeSourceID / EdgeTargetID carry the candidate IDs the relationship
// question is about. Reversing them produces a different expected verdict.
// They are opaque and free of ranker role tags.
type Fixture struct {
	ID                  string              `json:"id"`
	Task                Task                `json:"task"`
	SourceGroup         string              `json:"sourceGroup"`
	TemplateFamily      string              `json:"templateFamily,omitempty"`
	SourceRevision      SourceCommit        `json:"sourceRevision"`
	Question            string              `json:"question"`
	EdgeSourceID        string              `json:"edgeSourceId,omitempty"`
	EdgeTargetID        string              `json:"edgeTargetId,omitempty"`
	Excerpts            []Excerpt           `json:"excerpts"`
	Candidates          []Candidate         `json:"candidates"`
	CandidateGeneration string              `json:"candidateGeneration"`       // how candidates were produced
	CandidateSource     string              `json:"candidateSource,omitempty"` // free-text provenance for Candidates list (not silently dropped)
	AllowedLabels       []string            `json:"allowedLabels"`
	ExpectedLabel       string              `json:"expectedLabel"`
	SupportingSpans     []EvidenceSpan      `json:"supportingSpans"`
	Rationale           string              `json:"rationale"`
	RationaleEvidence   string              `json:"rationaleEvidence,omitempty"` // quoted evidence span or explicit-absence justification for the proposed label (not silently dropped)
	ChallengeCategories []ChallengeCategory `json:"challengeCategories"`
	Split               Split               `json:"split"`
	Author              string              `json:"author"`
	Reviewer            string              `json:"reviewer,omitempty"`
	ReviewStatus        ReviewStatus        `json:"reviewStatus"`
	NumericInvariants   []NumericInvariant  `json:"numericInvariants,omitempty"`
	AdversarialNote     string              `json:"adversarialNote,omitempty"`
}

// Manifest is the full corpus summary: fixtures, splits, source groups, and a
// per-file hash chain so reviewers can detect drift.
type Manifest struct {
	ProtocolVersion    string                    `json:"protocolVersion"`
	GeneratedAt        time.Time                 `json:"generatedAt"`
	FixtureFile        string                    `json:"fixtureFile"`
	FixtureCount       int                       `json:"fixtureCount"`
	SplitCounts        SplitCount                `json:"splitCounts"`
	TaskCounts         TaskCount                 `json:"taskCounts"`
	SourceGroupCount   int                       `json:"sourceGroupCount"`
	ReviewStatusCount  map[ReviewStatus]int      `json:"reviewStatusCount"`
	ChallengeCount     map[ChallengeCategory]int `json:"challengeCount"`
	SHA256FixtureFile  string                    `json:"sha256FixtureFile"`
	SHA256Manifest     string                    `json:"sha256Manifest"`
	FixtureHashes      []FixtureHash             `json:"fixtureHashes"`
	SplitIndependences []SplitGroupIndependence  `json:"splitIndependences"`
}

// SplitCount is a per-split total.
type SplitCount struct {
	Tuning  int `json:"tuning"`
	HoldOut int `json:"heldOut"`
	Total   int `json:"total"`
}

// TaskCount is a per-task total across splits.
type TaskCount struct {
	EntityType    int `json:"entityType"`
	CandidateType int `json:"candidateType"`
	Relationship  int `json:"relationship"`
	ClaimSupport  int `json:"claimSupport"`
	Total         int `json:"total"`
}

// FixtureHash records the SHA-256 over the canonical fixture record so a
// reviewer can detect silent drift in a fixture line.
type FixtureHash struct {
	ID    string `json:"id"`
	SHA   string `json:"sha256"`
	Split Split  `json:"split"`
}

// SplitGroupIndependence records what the split-independence check
// actually establishes. The harness does NOT claim statistical
// independence:
//   - Independent=true means group-IDs, template-family IDs, normalized
//     candidate names/aliases, extracted body-text entity names, and
//     candidate-to-body cross-channel identities are disjoint across splits.
//   - Repeated perturbations from the same family are not independent
//     samples. Sample-size sufficiency is reported separately from these
//     structural checks.
type SplitGroupIndependence struct {
	Split                           Split  `json:"split"`
	GroupCount                      int    `json:"sourceGroupCount"`
	Independent                     bool   `json:"independent"`
	ViolationNote                   string `json:"violationNote,omitempty"`
	EntityDisjoint                  bool   `json:"entityDisjoint"`
	EntityViolationNote             string `json:"entityViolationNote,omitempty"`
	BodyEntityDisjoint              bool   `json:"bodyEntityDisjoint"`
	BodyEntityViolationNote         string `json:"bodyEntityViolationNote,omitempty"`
	CrossChannelEntityDisjoint      bool   `json:"crossChannelEntityDisjoint"`
	CrossChannelEntityViolationNote string `json:"crossChannelEntityViolationNote,omitempty"`
	TemplateFamilyDisjoint          bool   `json:"templateFamilyDisjoint"`
	TemplateViolationNote           string `json:"templateViolationNote,omitempty"`
}

// Validate enforces structural rules on a fixture. It does not check label
// quality — that requires human review.
//
// AllowedLabels, when non-empty, is the reviewer-supplied vocabulary for
// this fixture. It may legitimately be broader than the closed task
// vocabulary (e.g. when a fixture exercises a candidate-typing task that
// uses an entity-role set distinct from the document-level vocabulary).
// When the fixture supplies AllowedLabels, membership in AllowedLabels
// satisfies the validator; otherwise membership in the closed task
// vocabulary is required.
func (f *Fixture) Validate() error {
	if !f.Task.Valid() {
		return fmt.Errorf("fixture %s: invalid task %q", f.ID, f.Task)
	}
	if f.ID == "" {
		return fmt.Errorf("fixture: missing id")
	}
	if f.SourceGroup == "" {
		return fmt.Errorf("fixture %s: missing sourceGroup", f.ID)
	}
	if f.Author == "" {
		return fmt.Errorf("fixture %s: missing author", f.ID)
	}
	if f.ExpectedLabel == "" {
		return fmt.Errorf("fixture %s: missing expectedLabel", f.ID)
	}
	allowed := f.AllowedLabels
	if len(allowed) == 0 {
		allowed = AllowedLabelsFor(f.Task)
	}
	if !containsString(allowed, f.ExpectedLabel) {
		return fmt.Errorf("fixture %s: expectedLabel %q not in vocabulary", f.ID, f.ExpectedLabel)
	}
	for _, l := range f.AllowedLabels {
		// A reviewer may legitimately extend the vocabulary; we only
		// complain when the fixture lists no overrides and uses the closed
		// vocabulary that doesn't accept the label.
		if len(f.AllowedLabels) == 0 && !IsAllowedLabel(f.Task, l) {
			return fmt.Errorf("fixture %s: allowedLabels contains %q not in vocabulary", f.ID, l)
		}
	}
	switch f.Split {
	case SplitTuning, SplitHeldOut, SplitReserved:
	default:
		return fmt.Errorf("fixture %s: invalid split %q", f.ID, f.Split)
	}
	for _, c := range f.ChallengeCategories {
		if !validChallenge(c) {
			return fmt.Errorf("fixture %s: invalid challenge %q", f.ID, c)
		}
	}
	if len(f.Excerpts) == 0 && !containsCategory(f.ChallengeCategories, ChallengeMissingEvidence) {
		return fmt.Errorf("fixture %s: missing excerpts and not tagged missing-evidence", f.ID)
	}
	// Relationship and claim-support fixtures must reference candidates by
	// ID. Candidate-typing fixtures must reference exactly one candidate
	// by ID (the entity being typed). Document-typing fixtures have no
	// candidate list (the document itself is the unit of classification).
	if f.Task == TaskCandidateType && len(f.Candidates) != 1 {
		return fmt.Errorf("fixture %s: candidate-typing fixture requires exactly one candidate", f.ID)
	}
	if f.Task != TaskEntityType && len(f.Candidates) == 0 {
		return fmt.Errorf("fixture %s: task %s requires candidates", f.ID, f.Task)
	}
	// Relationship fixtures must specify edge source/target IDs so a
	// reversed question can produce a different verdict.
	if f.Task == TaskRelationship {
		if f.EdgeSourceID == "" || f.EdgeTargetID == "" {
			return fmt.Errorf("fixture %s: relationship fixture requires edgeSourceId and edgeTargetId", f.ID)
		}
		if f.EdgeSourceID == f.EdgeTargetID {
			return fmt.Errorf("fixture %s: relationship fixture has identical source and target", f.ID)
		}
	}
	return nil
}

func containsString(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

func validChallenge(c ChallengeCategory) bool {
	switch c {
	case ChallengeStraightPositive, ChallengeStraightNegative,
		ChallengeMissingEvidence, ChallengeCompeting,
		ChallengeNegation, ChallengeReversedDir,
		ChallengeRenamed, ChallengeConflicting,
		ChallengeMisleading, ChallengeIrrelevant,
		ChallengeHarmlessEdit, ChallengeInvalidating,
		ChallengeAdversarial:
		return true
	}
	return false
}

func containsCategory(cs []ChallengeCategory, target ChallengeCategory) bool {
	for _, c := range cs {
		if c == target {
			return true
		}
	}
	return false
}

// DistinctSourceGroups returns the unique source group identifiers from a
// fixture slice, in deterministic sorted order. The split partitioner uses
// this to assign groups and the independence checker uses it to verify no
// group is split.
func DistinctSourceGroups(fs []Fixture) []string {
	set := make(map[string]struct{}, len(fs))
	for _, f := range fs {
		if f.SourceGroup == "" {
			continue
		}
		set[f.SourceGroup] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// LabelKey returns a stable identifier for label set membership, primarily
// for deterministic scoring logs.
func LabelKey(label string) string {
	return strings.ToLower(strings.TrimSpace(label))
}
