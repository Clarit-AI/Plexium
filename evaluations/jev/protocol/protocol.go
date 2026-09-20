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

// ProtocolVersion identifies the frozen state of the offline protocol. Change
// it whenever anything observable changes (vocabulary, scorer, adapter
// contract, transport rules, fixture review status).
const ProtocolVersion = "0.1.0"

// SourceCommit records the inspected source revision for a fixture or
// baseline. An empty value indicates a synthetic source group that does not
// pin a real upstream commit.
type SourceCommit struct {
	Repository string `json:"repository"`
	Revision   string `json:"revision"`
	Note       string `json:"note,omitempty"`
}

// Task identifies one of the three classification tasks the harness scores.
type Task string

const (
	TaskEntityType   Task = "entity-type"   // candidate document/entity typing
	TaskRelationship Task = "relationship"  // directed edge predicate classification
	TaskClaimSupport Task = "claim-support" // supported / contradicted / insufficient
)

func (t Task) Valid() bool {
	switch t {
	case TaskEntityType, TaskRelationship, TaskClaimSupport:
		return true
	}
	return false
}

// DocumentTypeLabel is the canonical whitelist for entity/document typing,
// kept separate from MarkedUp's `Entity.Role` which is the per-entity role
// assigned inside NER (e.g. PERSON, ORGANIZATION). The protocol document
// classification is a property of the page, not of an extracted entity.
//
// The first value ("document") is the default fallback and the only label
// the deterministic abstaining baseline emits.
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
}

// PredicateLabels is the seven generic relationship predicates plus the two
// abstain-class labels from protocol v0.1: no-supported-relationship and
// insufficient-evidence. Per the spec, the abstain labels are explicit
// vocabulary entries; "Negated edges are unsupported; absent evidence is
// insufficient; conflicting equally authoritative evidence is insufficient
// unless fixture provenance establishes precedence."
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
type Fixture struct {
	ID                  string              `json:"id"`
	Task                Task                `json:"task"`
	SourceGroup         string              `json:"sourceGroup"`
	SourceRevision      SourceCommit        `json:"sourceRevision"`
	Question            string              `json:"question"`
	Excerpts            []Excerpt           `json:"excerpts"`
	Candidates          []Candidate         `json:"candidates"`
	CandidateGeneration string              `json:"candidateGeneration"` // how candidates were produced
	AllowedLabels       []string            `json:"allowedLabels"`
	ExpectedLabel       string              `json:"expectedLabel"`
	SupportingSpans     []EvidenceSpan      `json:"supportingSpans"`
	Rationale           string              `json:"rationale"`
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
	EntityType   int `json:"entityType"`
	Relationship int `json:"relationship"`
	ClaimSupport int `json:"claimSupport"`
	Total        int `json:"total"`
}

// FixtureHash records the SHA-256 over the canonical fixture record so a
// reviewer can detect silent drift in a fixture line.
type FixtureHash struct {
	ID    string `json:"id"`
	SHA   string `json:"sha256"`
	Split Split  `json:"split"`
}

// SplitGroupIndependence proves a split partition does not leak source
// groups across partitions. A Split name is independent iff no source group
// appears in more than one partition and the total is the union.
type SplitGroupIndependence struct {
	Split         Split  `json:"split"`
	GroupCount    int    `json:"sourceGroupCount"`
	Independent   bool   `json:"independent"`
	ViolationNote string `json:"violationNote,omitempty"`
}

// Validate enforces structural rules on a fixture. It does not check label
// quality — that requires human review.
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
	if !IsAllowedLabel(f.Task, f.ExpectedLabel) {
		return fmt.Errorf("fixture %s: expectedLabel %q not in vocabulary", f.ID, f.ExpectedLabel)
	}
	for _, l := range f.AllowedLabels {
		if !IsAllowedLabel(f.Task, l) {
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
	// Relationship and claim-support fixtures must reference candidates by ID.
	if f.Task != TaskEntityType && len(f.Candidates) == 0 {
		return fmt.Errorf("fixture %s: task %s requires candidates", f.ID, f.Task)
	}
	return nil
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
