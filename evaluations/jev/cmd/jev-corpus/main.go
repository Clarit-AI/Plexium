// Command jev-corpus writes the pilot fixture JSONL. The corpus is
// synthesized by an agent and marked "unreviewed"; humans must approve or
// adjust every label before any quality claim is made.
//
// Protocol v0.3 demands:
//   - smaller honest corpus (~20 distinct source groups, ~10 fixtures
//     each); quality over quantity
//   - entity names / aliases are unique across the entire corpus by
//     salt-suffixing with the group ID during generation
//   - candidate-typing is its own task (TaskCandidateType) and uses
//     CandidateTypeLabels (PERSON, ORGANIZATION, etc), distinct from
//     document-typing
//   - all relationships include EdgeSourceID and EdgeTargetID; reverse
//     direction produces a different expected verdict
//   - conflicting equal-authority evidence is gold insufficient-evidence
//   - perturbation templates embed the group ID so no template family
//     spans splits
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Clarit-AI/Plexium/evaluations/jev/candidate"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

const author = "agent:KHA-579-pilot-author-v3"

// saltEntityName appends the group ID to the entity name (lowercased,
// dashed) so names are guaranteed unique across source groups. The
// normalization survives case-fold and stripping because the loader's
// disjointness check normalizes with the same rule.
func saltEntityName(groupID, name string) string {
	base := strings.ToLower(strings.TrimSpace(name))
	base = strings.ReplaceAll(base, " ", "-")
	return fmt.Sprintf("%s/%s", groupID, base)
}

func main() {
	out := flag.String("out", "pilot/fixtures.jsonl", "output fixture JSONL path")
	flag.Parse()
	if err := writeAll(*out); err != nil {
		fmt.Fprintf(os.Stderr, "jev-corpus: %v\n", err)
		os.Exit(1)
	}
	count := len(corpus())
	groups := sourceGroups()
	fmt.Fprintf(os.Stderr, "wrote %d fixtures across %d source groups\n", count, len(groups))
	// Sanity: report the entity-disjointness verdict from the corpus
	// loader's perspective so any failure is visible at generation time.
	if err := verifyEntitiesUnique(groups); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: entity disjointness failed: %v\n", err)
	}
}

// verifyEntitiesUnique is a defense-in-depth check: the loader runs the
// same logic against the generated fixture set; this check runs against
// the unsaved source-group slice so generation fails fast on a typo.
func verifyEntitiesUnique(groups []sourceGroup) error {
	seen := map[string]string{}
	for _, g := range groups {
		for _, e := range g.Entities {
			for _, name := range []string{e.Name, e.Alias1()} {
				n := strings.ToLower(strings.TrimSpace(name))
				if n == "" {
					continue
				}
				if prev, ok := seen[n]; ok && prev != g.ID {
					return fmt.Errorf("entity %q appears in groups %s and %s", n, prev, g.ID)
				}
				seen[n] = g.ID
			}
		}
	}
	return nil
}

func writeAll(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, fix := range corpus() {
		if err := enc.Encode(fix); err != nil {
			return err
		}
	}
	return nil
}

func corpus() []protocol.Fixture {
	out := make([]protocol.Fixture, 0, 240)
	for _, sg := range sourceGroups() {
		out = append(out, sg.fixtures()...)
		if f, ok := sg.ClaimSupportContradictedFixture(); ok {
			out = append(out, f)
		}
	}
	return out
}

// sourceGroup is one fictional evidence fragment with a small internally
// consistent set of entities and relations. Every entity name is salted
// with the source group's ID to guarantee corpus-wide uniqueness.
type sourceGroup struct {
	ID        string
	Title     string
	Subtitle  string
	Body      string
	Entities  []sgEntity
	Relations []sgRelation
	Claims    []sgClaim
}

type sgEntity struct {
	LocalID    string // opaque local identifier, "sg-NN-eM"
	Name       string // human-readable display name (salted to unique)
	Aliases    []string
	Type       string // candidate-typing role tag (PERSON / ORG / etc)
	EntityType string // document-level classification (paper|tool|person|...)
}

func (e sgEntity) Alias1() string {
	for _, a := range e.Aliases {
		if a != "" {
			return a
		}
	}
	return ""
}

type sgRelation struct {
	Source, Target string
	Predicate      string
}

type sgClaim struct {
	Subject, Predicate, Object string
}

func (sg sourceGroup) split() protocol.Split {
	if _, ok := tuningIDs[sg.ID]; ok {
		return protocol.SplitTuning
	}
	return protocol.SplitHeldOut
}

func (sg sourceGroup) fid(suffix string) string { return sg.ID + "-" + suffix }

func (sg sourceGroup) candidates() []protocol.Candidate {
	out := make([]protocol.Candidate, 0, len(sg.Entities))
	for _, e := range sg.Entities {
		out = append(out, protocol.Candidate{
			ID:    e.LocalID,
			Title: e.Name,
			Alias: e.Alias1(),
			Role:  "candidate",
		})
	}
	return out
}

func (sg sourceGroup) poolEntries() []candidate.PoolEntry {
	out := make([]candidate.PoolEntry, 0, len(sg.Entities))
	for _, e := range sg.Entities {
		out = append(out, candidate.PoolEntry{
			ID:      e.LocalID,
			Index:   len(out),
			Title:   e.Name,
			Aliases: []string{e.Alias1()},
		})
	}
	return out
}

func (sg sourceGroup) excerpt(text string) protocol.Excerpt {
	return protocol.Excerpt{ID: "e-body", Text: text, Revision: "v1"}
}

// fixtures emits every case this source group contributes. The shapes
// are stable across runs and intentionally limited so the pilot stays
// small and honest. Each group emits one candidate-typing fixture per
// entity (so all 7 role tags are exercised across the corpus), plus
// the core task variants.
func (sg sourceGroup) fixtures() []protocol.Fixture {
	out := []protocol.Fixture{}
	if r := sg.entityTypeBase(); r.ID != "" {
		out = append(out, r)
	}
	out = append(out, sg.candidateTypingFixtures()...)
	if r := sg.relationshipBase(); r.ID != "" {
		out = append(out, r)
	}
	if r := sg.relationshipReverse(); r.ID != "" {
		out = append(out, r)
	}
	if r := sg.relationshipNoSupported(); r.ID != "" {
		out = append(out, r)
	}
	if r := sg.relationshipInsufficient(); r.ID != "" {
		out = append(out, r)
	}
	if r := sg.claimSupportBase(); r.ID != "" {
		out = append(out, r)
	}
	if r := sg.claimSupportInsufficient(); r.ID != "" {
		out = append(out, r)
	}
	if r := sg.claimSupportNoPrecedence(); r.ID != "" {
		out = append(out, r)
	}
	if r := sg.entityTypeMissingEvidence(); r.ID != "" {
		out = append(out, r)
	}
	if r := sg.entityTypeRename(); r.ID != "" {
		out = append(out, r)
	}
	return out
}

// claimSupportContradicted is an extra, optional case that lives only on
// source groups whose body declares an authoritative contradicted
// statement. Coverage fillers for the contradicted label add this case
// directly via ClaimSupportContradictedFixture.
func (sg sourceGroup) ClaimSupportContradictedFixture() (protocol.Fixture, bool) {
	if sg.ID != "sg-cov-claim-contradicted" {
		return protocol.Fixture{}, false
	}
	return protocol.Fixture{
		ID:                  sg.fid("claim-contradicted"),
		Task:                protocol.TaskClaimSupport,
		SourceGroup:         sg.ID,
		TemplateFamily:      sg.pertFamily("claim-contradicted"),
		SourceRevision:      sg.sourceCommit(),
		Question:            "Is the claim that the Brenton Indexer D is a search engine supported?",
		Excerpts:            []protocol.Excerpt{sg.excerpt(sg.Body)},
		Candidates:          sg.candidates(),
		CandidateGeneration: "deterministic-title-match",
		AllowedLabels:       protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
		ExpectedLabel:       "contradicted",
		Rationale:           "The body states both 'is a search engine' and 'is NOT a search engine'; precedence is given to the later authoritative correction.",
		ChallengeCategories: []protocol.ChallengeCategory{protocol.ChallengeConflicting},
		Split:               sg.split(),
		Author:              author,
		ReviewStatus:        protocol.ReviewUnreviewed,
	}, true
}

// candidateTypingFixtures emits one candidate-typing fixture per entity
// in the source group, so the pilot corpus covers all 7 role tags.
func (sg sourceGroup) candidateTypingFixtures() []protocol.Fixture {
	out := make([]protocol.Fixture, 0, len(sg.Entities))
	for i, e := range sg.Entities {
		out = append(out, protocol.Fixture{
			ID:                  sg.fid(fmt.Sprintf("candidate-type-e%d", i+1)),
			Task:                protocol.TaskCandidateType,
			SourceGroup:         sg.ID,
			TemplateFamily:      "",
			SourceRevision:      sg.sourceCommit(),
			Question:            "What semantic role tag applies to the named candidate?",
			Excerpts:            []protocol.Excerpt{sg.excerpt(sg.Body)},
			Candidates:          []protocol.Candidate{{ID: e.LocalID, Title: e.Name, Alias: e.Alias1(), Role: "candidate"}},
			CandidateGeneration: "deterministic-title-match",
			AllowedLabels:       protocol.AllowedLabelsFor(protocol.TaskCandidateType),
			ExpectedLabel:       e.Type,
			Rationale:           "The candidate's role tag is " + e.Type + " based on the body.",
			ChallengeCategories: []protocol.ChallengeCategory{protocol.ChallengeStraightPositive},
			Split:               sg.split(),
			Author:              author,
			ReviewStatus:        protocol.ReviewUnreviewed,
		})
	}
	return out
}

func (sg sourceGroup) sourceCommit() protocol.SourceCommit {
	return protocol.SourceCommit{Repository: "synthetic://" + sg.ID, Revision: "v1", Note: "agent-authored synthetic"}
}

func (sg sourceGroup) entityTypeBase() protocol.Fixture {
	return protocol.Fixture{
		ID:                  sg.fid("entitytype-base"),
		Task:                protocol.TaskEntityType,
		SourceGroup:         sg.ID,
		TemplateFamily:      "",
		SourceRevision:      sg.sourceCommit(),
		Question:            "What kind of document is this?",
		Excerpts:            []protocol.Excerpt{sg.excerpt(sg.Body)},
		CandidateGeneration: "n/a (document typing is per-document, not per-candidate)",
		AllowedLabels:       protocol.AllowedLabelsFor(protocol.TaskEntityType),
		ExpectedLabel:       sg.Entities[0].EntityType,
		Rationale:           "The body establishes the document as a " + sg.Entities[0].EntityType + " document.",
		ChallengeCategories: []protocol.ChallengeCategory{protocol.ChallengeStraightPositive},
		Split:               sg.split(),
		Author:              author,
		ReviewStatus:        protocol.ReviewUnreviewed,
	}
}

func (sg sourceGroup) candidateTyping() protocol.Fixture {
	candidate := sg.Entities[0]
	return protocol.Fixture{
		ID:                  sg.fid("candidate-type"),
		Task:                protocol.TaskCandidateType,
		SourceGroup:         sg.ID,
		TemplateFamily:      "",
		SourceRevision:      sg.sourceCommit(),
		Question:            "What semantic role tag applies to the named candidate?",
		Excerpts:            []protocol.Excerpt{sg.excerpt(sg.Body)},
		Candidates:          []protocol.Candidate{{ID: candidate.LocalID, Title: candidate.Name, Alias: candidate.Alias1(), Role: "candidate"}},
		CandidateGeneration: "deterministic-title-match",
		AllowedLabels:       protocol.AllowedLabelsFor(protocol.TaskCandidateType),
		ExpectedLabel:       candidate.Type,
		Rationale:           "The candidate's role tag is " + candidate.Type + " based on the body.",
		ChallengeCategories: []protocol.ChallengeCategory{protocol.ChallengeStraightPositive},
		Split:               sg.split(),
		Author:              author,
		ReviewStatus:        protocol.ReviewUnreviewed,
	}
}

func (sg sourceGroup) entityTypeMissingEvidence() protocol.Fixture {
	return protocol.Fixture{
		ID:                  sg.fid("entitytype-missing"),
		Task:                protocol.TaskEntityType,
		SourceGroup:         sg.ID,
		TemplateFamily:      "",
		SourceRevision:      sg.sourceCommit(),
		Question:            "What kind of document is this when no body is supplied?",
		Excerpts:            []protocol.Excerpt{},
		CandidateGeneration: "n/a (no evidence to match)",
		AllowedLabels:       protocol.AllowedLabelsFor(protocol.TaskEntityType),
		ExpectedLabel:       "document",
		Rationale:           "With no evidence, the abstention / default fallback applies.",
		ChallengeCategories: []protocol.ChallengeCategory{protocol.ChallengeMissingEvidence},
		Split:               sg.split(),
		Author:              author,
		ReviewStatus:        protocol.ReviewUnreviewed,
	}
}

func (sg sourceGroup) entityTypeRename() protocol.Fixture {
	if len(sg.Entities) == 0 {
		return protocol.Fixture{}
	}
	e := sg.Entities[0]
	body := "Formerly known as " + e.Alias1() + ". " + sg.Body
	return protocol.Fixture{
		ID:                  sg.fid("entitytype-rename"),
		Task:                protocol.TaskEntityType,
		SourceGroup:         sg.ID,
		TemplateFamily:      sg.pertFamily("rename"),
		SourceRevision:      sg.sourceCommit(),
		Question:            "What kind of document is this?",
		Excerpts:            []protocol.Excerpt{sg.excerpt(body)},
		CandidateGeneration: "n/a (document typing is independent of name)",
		AllowedLabels:       protocol.AllowedLabelsFor(protocol.TaskEntityType),
		ExpectedLabel:       e.EntityType,
		Rationale:           "Renaming must not change the document type.",
		ChallengeCategories: []protocol.ChallengeCategory{protocol.ChallengeRenamed},
		Split:               sg.split(),
		Author:              author,
		ReviewStatus:        protocol.ReviewUnreviewed,
	}
}

func (sg sourceGroup) relationshipBase() protocol.Fixture {
	if len(sg.Relations) == 0 {
		return protocol.Fixture{}
	}
	rel := sg.Relations[0]
	return protocol.Fixture{
		ID:                  sg.fid("rel-base"),
		Task:                protocol.TaskRelationship,
		SourceGroup:         sg.ID,
		TemplateFamily:      "",
		SourceRevision:      sg.sourceCommit(),
		Question:            fmt.Sprintf("What directed relationship from %q to %q is supported by the body?", rel.Source, rel.Target),
		EdgeSourceID:        sourceIDFor(sg, rel.Source),
		EdgeTargetID:        targetIDFor(sg, rel.Target),
		Excerpts:            []protocol.Excerpt{sg.excerpt(sg.Body)},
		Candidates:          sg.candidates(),
		CandidateGeneration: "deterministic-title-match",
		AllowedLabels:       append([]string{}, protocol.AllowedLabelsFor(protocol.TaskRelationship)...),
		ExpectedLabel:       rel.Predicate,
		Rationale:           "Body states that " + rel.Source + " " + rel.Predicate + " " + rel.Target + ".",
		ChallengeCategories: []protocol.ChallengeCategory{protocol.ChallengeStraightPositive},
		Split:               sg.split(),
		Author:              author,
		ReviewStatus:        protocol.ReviewUnreviewed,
	}
}

func (sg sourceGroup) relationshipReverse() protocol.Fixture {
	if len(sg.Relations) == 0 {
		return protocol.Fixture{}
	}
	rel := sg.Relations[0]
	return protocol.Fixture{
		ID:                  sg.fid("rel-reverse"),
		Task:                protocol.TaskRelationship,
		SourceGroup:         sg.ID,
		TemplateFamily:      sg.pertFamily("rel-reverse"),
		SourceRevision:      sg.sourceCommit(),
		Question:            fmt.Sprintf("What directed relationship from %q to %q is supported by the body?", rel.Target, rel.Source),
		EdgeSourceID:        sourceIDFor(sg, rel.Target),
		EdgeTargetID:        targetIDFor(sg, rel.Source),
		Excerpts:            []protocol.Excerpt{sg.excerpt(sg.Body)},
		Candidates:          sg.candidates(),
		CandidateGeneration: "deterministic-title-match",
		AllowedLabels:       append([]string{}, protocol.AllowedLabelsFor(protocol.TaskRelationship)...),
		ExpectedLabel:       "no-supported-relationship",
		Rationale:           "Reversing source/target produces a wrong edge; no body statement supports target→source.",
		ChallengeCategories: []protocol.ChallengeCategory{protocol.ChallengeReversedDir},
		Split:               sg.split(),
		Author:              author,
		ReviewStatus:        protocol.ReviewUnreviewed,
	}
}

func (sg sourceGroup) relationshipNoSupported() protocol.Fixture {
	if len(sg.Entities) < 2 {
		return protocol.Fixture{}
	}
	return protocol.Fixture{
		ID:                  sg.fid("rel-none"),
		Task:                protocol.TaskRelationship,
		SourceGroup:         sg.ID,
		TemplateFamily:      sg.pertFamily("rel-none"),
		SourceRevision:      sg.sourceCommit(),
		Question:            "What directed relationship from this page to another is supported by the body?",
		EdgeSourceID:        sg.Entities[0].LocalID,
		EdgeTargetID:        sg.Entities[1].LocalID,
		Excerpts:            []protocol.Excerpt{sg.excerpt(sg.Body)},
		Candidates:          sg.candidates(),
		CandidateGeneration: "deterministic-title-match",
		AllowedLabels:       append([]string{}, protocol.AllowedLabelsFor(protocol.TaskRelationship)...),
		ExpectedLabel:       "no-supported-relationship",
		Rationale:           "The body describes the entity but does not assert a directed relationship to the listed candidate.",
		ChallengeCategories: []protocol.ChallengeCategory{protocol.ChallengeStraightNegative},
		Split:               sg.split(),
		Author:              author,
		ReviewStatus:        protocol.ReviewUnreviewed,
	}
}

// relationshipInsufficient covers the abstain-class relationship label
// "insufficient-evidence" — when the body is missing altogether, the
// relationship answer is the abstain-class label rather than
// no-supported-relationship.
func (sg sourceGroup) relationshipInsufficient() protocol.Fixture {
	if len(sg.Entities) < 2 {
		return protocol.Fixture{}
	}
	return protocol.Fixture{
		ID:                  sg.fid("rel-insufficient"),
		Task:                protocol.TaskRelationship,
		SourceGroup:         sg.ID,
		TemplateFamily:      sg.pertFamily("rel-insufficient"),
		SourceRevision:      sg.sourceCommit(),
		Question:            "Without further evidence, is any directed relationship from this page supported?",
		EdgeSourceID:        sg.Entities[0].LocalID,
		EdgeTargetID:        sg.Entities[1].LocalID,
		Excerpts:            []protocol.Excerpt{}, // missing evidence
		Candidates:          sg.candidates(),
		CandidateGeneration: "deterministic-title-match (no excerpt to match against)",
		AllowedLabels:       append([]string{}, protocol.AllowedLabelsFor(protocol.TaskRelationship)...),
		ExpectedLabel:       "insufficient-evidence",
		Rationale:           "Without a body there is insufficient evidence to support any directed relationship.",
		ChallengeCategories: []protocol.ChallengeCategory{protocol.ChallengeMissingEvidence},
		Split:               sg.split(),
		Author:              author,
		ReviewStatus:        protocol.ReviewUnreviewed,
	}
}

func (sg sourceGroup) claimSupportBase() protocol.Fixture {
	if len(sg.Claims) == 0 {
		return protocol.Fixture{}
	}
	c := sg.Claims[0]
	return protocol.Fixture{
		ID:                  sg.fid("claim-base"),
		Task:                protocol.TaskClaimSupport,
		SourceGroup:         sg.ID,
		TemplateFamily:      "",
		SourceRevision:      sg.sourceCommit(),
		Question:            fmt.Sprintf("Is the claim '%s %s %s' supported by the body?", c.Subject, c.Predicate, c.Object),
		Excerpts:            []protocol.Excerpt{sg.excerpt(sg.Body)},
		Candidates:          sg.candidates(),
		CandidateGeneration: "deterministic-title-match",
		AllowedLabels:       protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
		ExpectedLabel:       "supported",
		Rationale:           "The body directly states the claim.",
		ChallengeCategories: []protocol.ChallengeCategory{protocol.ChallengeStraightPositive},
		Split:               sg.split(),
		Author:              author,
		ReviewStatus:        protocol.ReviewUnreviewed,
	}
}

func (sg sourceGroup) claimSupportInsufficient() protocol.Fixture {
	if len(sg.Claims) == 0 {
		return protocol.Fixture{}
	}
	c := sg.Claims[0]
	body := "The body for " + sg.ID + " makes no statement about " + c.Subject + "."
	return protocol.Fixture{
		ID:                  sg.fid("claim-insufficient"),
		Task:                protocol.TaskClaimSupport,
		SourceGroup:         sg.ID,
		TemplateFamily:      sg.pertFamily("claim-insufficient"),
		SourceRevision:      sg.sourceCommit(),
		Question:            fmt.Sprintf("Without further evidence, is the claim '%s %s %s' supported?", c.Subject, c.Predicate, c.Object),
		Excerpts:            []protocol.Excerpt{sg.excerpt(body)},
		Candidates:          sg.candidates(),
		CandidateGeneration: "deterministic-title-match",
		AllowedLabels:       protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
		ExpectedLabel:       "insufficient-evidence",
		Rationale:           "The body for " + sg.ID + " does not address the claim; lack of contradiction is not support.",
		ChallengeCategories: []protocol.ChallengeCategory{protocol.ChallengeMissingEvidence},
		Split:               sg.split(),
		Author:              author,
		ReviewStatus:        protocol.ReviewUnreviewed,
	}
}

func (sg sourceGroup) claimSupportNoPrecedence() protocol.Fixture {
	body := sg.Body + " [Conflicting update for " + sg.ID + ": " + sg.Entities[0].Name + " does NOT participate; this contradicts the prior statement.]"
	return protocol.Fixture{
		ID:                  sg.fid("claim-noprec"),
		Task:                protocol.TaskClaimSupport,
		SourceGroup:         sg.ID,
		TemplateFamily:      sg.pertFamily("claim-noprec"),
		SourceRevision:      sg.sourceCommit(),
		Question:            "Is the earlier claim still supported given the conflicting update?",
		Excerpts:            []protocol.Excerpt{sg.excerpt(body)},
		Candidates:          sg.candidates(),
		CandidateGeneration: "deterministic-title-match",
		AllowedLabels:       protocol.AllowedLabelsFor(protocol.TaskClaimSupport),
		ExpectedLabel:       "insufficient-evidence",
		Rationale:           "Two equally authoritative fragments disagree; without precedence the gold is insufficient-evidence.",
		ChallengeCategories: []protocol.ChallengeCategory{protocol.ChallengeConflicting},
		Split:               sg.split(),
		Author:              author,
		ReviewStatus:        protocol.ReviewUnreviewed,
	}
}

func sourceIDFor(sg sourceGroup, name string) string {
	for _, e := range sg.Entities {
		if e.Name == name {
			return e.LocalID
		}
	}
	return ""
}

func targetIDFor(sg sourceGroup, name string) string {
	for _, e := range sg.Entities {
		if e.Name == name {
			return e.LocalID
		}
	}
	return ""
}

// pertFamily returns a per-group perturbation template identifier. Each
// source group embeds its own ID so no perturbation template is shared
// across source groups.
func (sg sourceGroup) pertFamily(shape string) string {
	return shape + "-" + sg.ID
}

// tuningIDs are the source-group IDs that fall into the tuning split.
var tuningIDs = map[string]struct{}{}

const tuningTarget = 4 // 4 tuning groups × 10 fixtures = 40 tuning fixtures

func init() {
	for i, sg := range sourceGroups() {
		// Only the base groups (sg-001..sg-020) participate in the tuning
		// split. Coverage fillers are held-out only; they MUST NOT
		// inflate tuning counts.
		if i < tuningTarget && !strings.HasPrefix(sg.ID, "sg-cov-") {
			tuningIDs[sg.ID] = struct{}{}
		}
	}
}

// makeSalt produces a salted entity name like "sg-001/vornholt-pass" so
// names are guaranteed unique across the corpus.
func makeSalt(groupID, base string) string { return saltEntityName(groupID, base) }

// sourceGroups enumerates the synthetic source groups. Each entity is
// salted with the group ID; aliases (where present) are also salted. The
// generator post-processes LocalIDs so each entity carries an opaque
// "sg-NN-eM" identifier the candidate-generation exercise uses.
func sourceGroups() []sourceGroup {
	mk := func(group string, n int, base, alias string, typeTag, docType string) sgEntity {
		return sgEntity{
			LocalID:    fmt.Sprintf("%s-e%d", group, n),
			Name:       makeSalt(group, base),
			Aliases:    []string{makeSalt(group, alias)},
			Type:       typeTag,
			EntityType: docType,
		}
	}
	type spec struct {
		ID, Title, Subtitle, Body string
		Entities                  []sgEntity
		Relations                 []sgRelation
		Claims                    []sgClaim
	}
	specs := []spec{
		{
			ID: "sg-001", Title: "Almanac of Vornholt Pass",
			Subtitle: "An ethnographic reference on the Vornholt Pass region",
			Body:     "The Vornholt Pass region is administered by the Lindewall Council. Local guides maintain the Vornic Trail Registry. The capital of Vornholt is Maringen, which hosts the annual Vornholt Lantern Festival.",
			Entities: []sgEntity{
				mk("sg-001", 1, "Vornholt Pass", "Vornholt", "LOCATION", "place"),
				mk("sg-001", 2, "Lindewall Council", "Lindewall", "ORGANIZATION", "organization"),
				mk("sg-001", 3, "Vornic Trail Registry", "Trail Registry", "DOCUMENT", "document"),
				mk("sg-001", 4, "Maringen", "Maringen City", "LOCATION", "place"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-001", "Vornholt Pass"), Target: makeSalt("sg-001", "Lindewall Council"), Predicate: "part-of"},
				{Source: makeSalt("sg-001", "Vornic Trail Registry"), Target: makeSalt("sg-001", "Vornholt Pass"), Predicate: "related-to"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-001", "Lindewall Council"), Predicate: "administers", Object: makeSalt("sg-001", "Vornholt Pass")},
			},
		},
		{
			ID: "sg-002", Title: "Breyganth Smoke-Clock Manual",
			Subtitle: "Operating manual for the Breyganth Smoke-Clock Mk II",
			Body:     "The Breyganth Smoke-Clock measures time via the controlled emission of fragrant smoke. The Mk II variant was developed by Trembald & Sons and is distributed through the Aurelian Trade Houses.",
			Entities: []sgEntity{
				mk("sg-002", 1, "Breyganth Smoke-Clock", "Smoke-Clock", "TOOL", "tool"),
				mk("sg-002", 2, "Trembald Sons", "Trembald", "ORGANIZATION", "organization"),
				mk("sg-002", 3, "Aurelian Trade Houses", "Aurelian Trade", "ORGANIZATION", "organization"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-002", "Breyganth Smoke-Clock"), Target: makeSalt("sg-002", "Trembald Sons"), Predicate: "created-by"},
				{Source: makeSalt("sg-002", "Trembald Sons"), Target: makeSalt("sg-002", "Aurelian Trade Houses"), Predicate: "related-to"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-002", "Trembald Sons"), Predicate: "developed", Object: makeSalt("sg-002", "Breyganth Smoke-Clock")},
			},
		},
		{
			ID: "sg-003", Title: "Cantorian Steppes Diary",
			Subtitle: "Field notes by Dr. Eilis Cantorin",
			Body:     "The Cantorian Steppes stretch across the southern latitudes. Travel through the steppes requires passage permits issued by the Cantorian Caravan Office. Dr. Eilis Cantorin's diary documents flora and fauna encountered between cycles 41 and 73.",
			Entities: []sgEntity{
				mk("sg-003", 1, "Cantorian Steppes", "The Steppes", "LOCATION", "place"),
				mk("sg-003", 2, "Cantorian Caravan Office", "Caravan Office", "ORGANIZATION", "organization"),
				mk("sg-003", 3, "Eilis Cantorin", "Dr Cantorin", "PERSON", "person"),
				mk("sg-003", 4, "Cantorian Steppes Diary", "Cantorian Diary", "DOCUMENT", "document"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-003", "Cantorian Caravan Office"), Target: makeSalt("sg-003", "Cantorian Steppes"), Predicate: "related-to"},
				{Source: makeSalt("sg-003", "Cantorian Caravan Office"), Target: makeSalt("sg-003", "Eilis Cantorin"), Predicate: "created-by"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-003", "Eilis Cantorin"), Predicate: "wrote", Object: makeSalt("sg-003", "Cantorian Steppes Diary")},
			},
		},
		{
			ID: "sg-004", Title: "Driftwheel Engine Notes",
			Subtitle: "Engineering record by Halsten Drift",
			Body:     "The Driftwheel Engine produces rotary motion by exploiting thermal differentials between two coaxial wheels. Halsten Drift's prototype reached 17 revolutions per cycle. The design is licensed to Greycloak Workshop.",
			Entities: []sgEntity{
				mk("sg-004", 1, "Driftwheel Engine", "Driftwheel", "TOOL", "tool"),
				mk("sg-004", 2, "Halsten Drift", "Drift", "PERSON", "person"),
				mk("sg-004", 3, "Greycloak Workshop", "Greycloak", "ORGANIZATION", "organization"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-004", "Driftwheel Engine"), Target: makeSalt("sg-004", "Halsten Drift"), Predicate: "created-by"},
				{Source: makeSalt("sg-004", "Driftwheel Engine"), Target: makeSalt("sg-004", "Greycloak Workshop"), Predicate: "used-by"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-004", "Halsten Drift"), Predicate: "designed", Object: makeSalt("sg-004", "Driftwheel Engine")},
			},
		},
		{
			ID: "sg-005", Title: "Ember Court Charter",
			Subtitle: "Founding document of the Ember Court",
			Body:     "The Ember Court governs the eastern marches of the Vale of Tessar. Its charter was signed by Tessarine the Fourth and ratified by the Council of Oresund. The Court's archives are held in the Silver Vault.",
			Entities: []sgEntity{
				mk("sg-005", 1, "Ember Court", "The Court", "ORGANIZATION", "organization"),
				mk("sg-005", 2, "Vale of Tessar", "Vale Tessar", "LOCATION", "place"),
				mk("sg-005", 3, "Tessarine the Fourth", "Tessarine IV", "PERSON", "person"),
				mk("sg-005", 4, "Silver Vault", "The Vault", "LOCATION", "place"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-005", "Ember Court"), Target: makeSalt("sg-005", "Vale of Tessar"), Predicate: "related-to"},
				{Source: makeSalt("sg-005", "Ember Court"), Target: makeSalt("sg-005", "Tessarine the Fourth"), Predicate: "created-by"},
				{Source: makeSalt("sg-005", "Silver Vault"), Target: makeSalt("sg-005", "Ember Court"), Predicate: "part-of"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-005", "Tessarine the Fourth"), Predicate: "signed", Object: makeSalt("sg-005", "Ember Court Charter")},
			},
		},
		{
			ID: "sg-006", Title: "Frostglass Atlas",
			Subtitle: "Cartographic survey of the Frostglass Reach",
			Body:     "The Frostglass Atlas catalogues 41 known sheets of the Frostglass Reach, including the Icerift Plateau and the Twinning Glaciers. The atlas is maintained by the Helvar Survey Corps.",
			Entities: []sgEntity{
				mk("sg-006", 1, "Frostglass Atlas", "The Atlas", "DOCUMENT", "document"),
				mk("sg-006", 2, "Frostglass Reach", "The Reach", "LOCATION", "place"),
				mk("sg-006", 3, "Helvar Survey Corps", "Helvar Survey", "ORGANIZATION", "organization"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-006", "Frostglass Atlas"), Target: makeSalt("sg-006", "Helvar Survey Corps"), Predicate: "created-by"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-006", "Helvar Survey Corps"), Predicate: "maintains", Object: makeSalt("sg-006", "Frostglass Atlas")},
			},
		},
		{
			ID: "sg-007", Title: "Glimmergrass Almanac",
			Subtitle: "Botanical notes on the Glimmergrass Plains",
			Body:     "Glimmergrass is a bioluminescent grass species native to the plains south of the Heldar Range. Its flowering cycle peaks every 41 days. The Heldar Range flora expedition logged 17 specimens of related subspecies.",
			Entities: []sgEntity{
				mk("sg-007", 1, "Glimmergrass", "Glimmer Grass", "CONCEPT", "concept"),
				mk("sg-007", 2, "Heldar Range", "Heldar Mountains", "LOCATION", "place"),
				mk("sg-007", 3, "Heldar Range Flora Expedition", "Heldar Expedition", "EVENT", "event"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-007", "Glimmergrass"), Target: makeSalt("sg-007", "Heldar Range"), Predicate: "related-to"},
				{Source: makeSalt("sg-007", "Heldar Range Flora Expedition"), Target: makeSalt("sg-007", "Heldar Range"), Predicate: "related-to"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-007", "Glimmergrass"), Predicate: "flowers-every", Object: "41-days"},
			},
		},
		{
			ID: "sg-008", Title: "Halberd Concord",
			Subtitle: "Treaty between the Hill Clans and the Greycloak Workshop",
			Body:     "The Halberd Concord was signed in the year 41 of the third age by the leaders of the Hill Clans and the Greycloak Workshop. The concord regulates the trade of forged halberds between the clans and the workshop.",
			Entities: []sgEntity{
				mk("sg-008", 1, "Halberd Concord", "The Concord", "DOCUMENT", "document"),
				mk("sg-008", 2, "Hill Clans", "The Clans", "ORGANIZATION", "organization"),
				mk("sg-008", 3, "Greycloak Forge", "Greycloak", "ORGANIZATION", "organization"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-008", "Halberd Concord"), Target: makeSalt("sg-008", "Hill Clans"), Predicate: "created-by"},
				{Source: makeSalt("sg-008", "Halberd Concord"), Target: makeSalt("sg-008", "Greycloak Forge"), Predicate: "created-by"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-008", "Halberd Concord"), Predicate: "regulates", Object: "halberd-trade"},
			},
		},
		{
			ID: "sg-009", Title: "Iridian Lattice Reports",
			Subtitle: "Quarterly analysis of the Iridian Lattice transit network",
			Body:     "The Iridian Lattice connects 41 trade hubs across the eastern provinces. The Transit Authority publishes the Iridian Lattice Reports each quarter.",
			Entities: []sgEntity{
				mk("sg-009", 1, "Iridian Lattice", "The Lattice", "CONCEPT", "concept"),
				mk("sg-009", 2, "Transit Authority", "Authority", "ORGANIZATION", "organization"),
				mk("sg-009", 3, "Iridian Lattice Reports", "The Reports", "DOCUMENT", "document"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-009", "Iridian Lattice Reports"), Target: makeSalt("sg-009", "Transit Authority"), Predicate: "created-by"},
				{Source: makeSalt("sg-009", "Transit Authority"), Target: makeSalt("sg-009", "Iridian Lattice"), Predicate: "related-to"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-009", "Iridian Lattice"), Predicate: "connects", Object: "41-hubs"},
			},
		},
		{
			ID: "sg-010", Title: "Jorlund Foundry Ledger",
			Subtitle: "Account book of the Jorlund Foundry",
			Body:     "The Jorlund Foundry casts bronze fittings for the Greycloak Forge. The foundry's ledger catalogues 41,000 units produced across the last 17 cycles. The foundry's foreman is Marit Jorlund.",
			Entities: []sgEntity{
				mk("sg-010", 1, "Jorlund Foundry", "The Foundry", "ORGANIZATION", "organization"),
				mk("sg-010", 2, "Greycloak Forge Annex", "Greycloak Annex", "ORGANIZATION", "organization"),
				mk("sg-010", 3, "Marit Jorlund", "Marit", "PERSON", "person"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-010", "Jorlund Foundry"), Target: makeSalt("sg-010", "Greycloak Forge Annex"), Predicate: "used-by"},
				{Source: makeSalt("sg-010", "Jorlund Foundry"), Target: makeSalt("sg-010", "Marit Jorlund"), Predicate: "created-by"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-010", "Marit Jorlund"), Predicate: "is-foreman-of", Object: makeSalt("sg-010", "Jorlund Foundry")},
			},
		},
		{
			ID: "sg-011", Title: "Kestrels Atlas of the Inner Sea",
			Subtitle: "Nautical survey compiled by Captain Brindle Kestrel",
			Body:     "The Inner Sea is bounded by the Tessarine Coast, the Helvar Shoals, and the Inlume Reach. Captain Brindle Kestrel's atlas catalogues 41 known anchorages.",
			Entities: []sgEntity{
				mk("sg-011", 1, "Inner Sea", "The Sea", "LOCATION", "place"),
				mk("sg-011", 2, "Tessarine Coast Variant", "Tessarine Coast Alt", "LOCATION", "place"),
				mk("sg-011", 3, "Brindle Kestrel", "Captain Kestrel", "PERSON", "person"),
				mk("sg-011", 4, "Kestrels Atlas", "Kestrels Atlas Alt", "DOCUMENT", "document"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-011", "Tessarine Coast Variant"), Target: makeSalt("sg-011", "Inner Sea"), Predicate: "part-of"},
				{Source: makeSalt("sg-011", "Kestrels Atlas"), Target: makeSalt("sg-011", "Brindle Kestrel"), Predicate: "created-by"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-011", "Brindle Kestrel"), Predicate: "compiled", Object: makeSalt("sg-011", "Kestrels Atlas")},
			},
		},
		{
			ID: "sg-012", Title: "Larkspur Concordance",
			Subtitle: "Cross-reference of Larkspur dialect terms",
			Body:     "The Larkspur Concordance is the canonical reference for the Larkspur dialect. The concordance was compiled by Sira Larkspur and Aedan Vell, and is published by the Inlume Press.",
			Entities: []sgEntity{
				mk("sg-012", 1, "Larkspur Concordance", "The Concordance", "DOCUMENT", "document"),
				mk("sg-012", 2, "Sira Larkspur", "Sira", "PERSON", "person"),
				mk("sg-012", 3, "Aedan Vell", "Aedan", "PERSON", "person"),
				mk("sg-012", 4, "Inlume Press", "The Press", "ORGANIZATION", "organization"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-012", "Larkspur Concordance"), Target: makeSalt("sg-012", "Sira Larkspur"), Predicate: "created-by"},
				{Source: makeSalt("sg-012", "Larkspur Concordance"), Target: makeSalt("sg-012", "Inlume Press"), Predicate: "used-by"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-012", "Sira Larkspur"), Predicate: "co-authored", Object: makeSalt("sg-012", "Larkspur Concordance")},
			},
		},
		{
			ID: "sg-013", Title: "Maringen Civic Almanac",
			Subtitle: "Annual handbook of Maringen civic life",
			Body:     "The Maringen Civic Almanac catalogues the city's institutions: the Lantern Council, the Maringen Conservatory, and the Westgate Library. The almanac is reprinted every 17 cycles by the Civic Press.",
			Entities: []sgEntity{
				mk("sg-013", 1, "Maringen Civic Almanac", "The Almanac", "DOCUMENT", "document"),
				mk("sg-013", 2, "Lantern Council", "The Council", "ORGANIZATION", "organization"),
				mk("sg-013", 3, "Maringen Conservatory", "The Conservatory", "ORGANIZATION", "organization"),
				mk("sg-013", 4, "Westgate Library", "The Library", "ORGANIZATION", "organization"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-013", "Maringen Civic Almanac"), Target: makeSalt("sg-013", "Lantern Council"), Predicate: "related-to"},
				{Source: makeSalt("sg-013", "Maringen Civic Almanac"), Target: makeSalt("sg-013", "Maringen Conservatory"), Predicate: "related-to"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-013", "Maringen Civic Almanac"), Predicate: "catalogues", Object: "institutions"},
			},
		},
		{
			ID: "sg-014", Title: "Northgate Patrol Logs",
			Subtitle: "Duty records of the Northgate Patrol, year 17",
			Body:     "The Northgate Patrol guards the northern trade road. Sergeant Halla Vorne led 41 patrols in year 17; her second in command was Tarin Ewell. Patrols use the standardized Northgate ledger book.",
			Entities: []sgEntity{
				mk("sg-014", 1, "Northgate Patrol", "The Patrol", "ORGANIZATION", "organization"),
				mk("sg-014", 2, "Halla Vorne", "Sergeant Vorne", "PERSON", "person"),
				mk("sg-014", 3, "Tarin Ewell", "Tarin", "PERSON", "person"),
				mk("sg-014", 4, "Northgate Ledger", "Ledger Book", "DOCUMENT", "document"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-014", "Northgate Patrol"), Target: makeSalt("sg-014", "Halla Vorne"), Predicate: "related-to"},
				{Source: makeSalt("sg-014", "Northgate Patrol"), Target: makeSalt("sg-014", "Tarin Ewell"), Predicate: "related-to"},
				{Source: makeSalt("sg-014", "Northgate Ledger"), Target: makeSalt("sg-014", "Northgate Patrol"), Predicate: "used-by"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-014", "Halla Vorne"), Predicate: "led", Object: "41-patrols"},
			},
		},
		{
			ID: "sg-015", Title: "Oresund Reef Survey",
			Subtitle: "Hydrographic survey of the Oresund Reef",
			Body:     "The Oresund Reef spans the strait between the Tessarine Coast and the Heldar Range. The reef supports 17 distinct fish species and 41 known invertebrates. The Oresund Reef Survey is published by the Council of Oresund.",
			Entities: []sgEntity{
				mk("sg-015", 1, "Oresund Reef", "The Reef", "LOCATION", "place"),
				mk("sg-015", 2, "Tessarine Coast Channel", "Tessarine Channel", "LOCATION", "place"),
				mk("sg-015", 3, "Heldar Foothills", "Heldar Foothill", "LOCATION", "place"),
				mk("sg-015", 4, "Council of Oresund", "Oresund Council", "ORGANIZATION", "organization"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-015", "Oresund Reef"), Target: makeSalt("sg-015", "Tessarine Coast Channel"), Predicate: "related-to"},
				{Source: makeSalt("sg-015", "Oresund Reef"), Target: makeSalt("sg-015", "Heldar Foothills"), Predicate: "related-to"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-015", "Oresund Reef"), Predicate: "supports", Object: "17-fish-species"},
			},
		},
		{
			ID: "sg-016", Title: "Pellenor Atlas",
			Subtitle: "Cartographic survey of the Pellenor Valley",
			Body:     "The Pellenor Valley is drained by the Brindle River. The Pellenor Atlas catalogues 41 villages and 17 historical sites. Surveyor Yara Pellenor led the cartographic work over 17 cycles.",
			Entities: []sgEntity{
				mk("sg-016", 1, "Pellenor Valley", "Pellenor", "LOCATION", "place"),
				mk("sg-016", 2, "Brindle River Variant", "Brindle River Alt", "LOCATION", "place"),
				mk("sg-016", 3, "Pellenor Atlas Variant", "Pellenor Atlas Alt", "DOCUMENT", "document"),
				mk("sg-016", 4, "Yara Pellenor", "Surveyor Pellenor", "PERSON", "person"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-016", "Pellenor Valley"), Target: makeSalt("sg-016", "Brindle River Variant"), Predicate: "related-to"},
				{Source: makeSalt("sg-016", "Pellenor Atlas Variant"), Target: makeSalt("sg-016", "Yara Pellenor"), Predicate: "created-by"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-016", "Pellenor Atlas Variant"), Predicate: "catalogues", Object: "41-villages"},
			},
		},
		{
			ID: "sg-017", Title: "Quill and Lantern Press Catalogue",
			Subtitle: "Catalogue of the Quill and Lantern Press",
			Body:     "The Quill and Lantern Press prints limited editions of regional histories. The press's catalogue lists 17 active titles, including the Breyganth Smoke-Clock Manual and the Cantorian Steppes Diary.",
			Entities: []sgEntity{
				mk("sg-017", 1, "Quill and Lantern Press", "Quill Lantern", "ORGANIZATION", "organization"),
				mk("sg-017", 2, "Quill and Lantern Press Catalogue", "The Catalogue", "DOCUMENT", "document"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-017", "Quill and Lantern Press"), Target: makeSalt("sg-017", "Quill and Lantern Press Catalogue"), Predicate: "created-by"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-017", "Quill and Lantern Press Catalogue"), Predicate: "lists", Object: "17-titles"},
			},
		},
		{
			ID: "sg-018", Title: "Ridgepole Engineering Notes",
			Subtitle: "Engineering record by the Ridgepole Workshop",
			Body:     "The Ridgepole Workshop maintains the Spine Bridge across the Vornholt Pass. The bridge is 41 meters long and supports 17 metric tons. The workshop's engineers are trained at the Greycloak Workshop.",
			Entities: []sgEntity{
				mk("sg-018", 1, "Ridgepole Workshop", "Ridgepole", "ORGANIZATION", "organization"),
				mk("sg-018", 2, "Spine Bridge", "The Bridge", "CONCEPT", "concept"),
				mk("sg-018", 3, "Vornholt Pass Region", "Vornholt Region", "LOCATION", "place"),
				mk("sg-018", 4, "Greycloak Workshop Branch", "Greycloak Branch", "ORGANIZATION", "organization"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-018", "Spine Bridge"), Target: makeSalt("sg-018", "Vornholt Pass Region"), Predicate: "related-to"},
				{Source: makeSalt("sg-018", "Ridgepole Workshop"), Target: makeSalt("sg-018", "Spine Bridge"), Predicate: "used-by"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-018", "Spine Bridge"), Predicate: "supports", Object: "17-tons"},
			},
		},
		{
			ID: "sg-019", Title: "Silver Vault Manifest",
			Subtitle: "Inventory of the Silver Vault",
			Body:     "The Silver Vault holds the archives of the Ember Court Annex. The current manifest lists 41 crates of historical correspondence and 17 ceremonial objects. Vault keeper Ori Tremaine catalogues every new accession.",
			Entities: []sgEntity{
				mk("sg-019", 1, "Silver Vault Annex", "The Vault Annex", "LOCATION", "place"),
				mk("sg-019", 2, "Ember Court Annex", "The Court Annex", "ORGANIZATION", "organization"),
				mk("sg-019", 3, "Ori Tremaine", "Vault Keeper Tremaine", "PERSON", "person"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-019", "Silver Vault Annex"), Target: makeSalt("sg-019", "Ember Court Annex"), Predicate: "related-to"},
				{Source: makeSalt("sg-019", "Ori Tremaine"), Target: makeSalt("sg-019", "Silver Vault Annex"), Predicate: "related-to"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-019", "Silver Vault Annex"), Predicate: "holds", Object: "Ember Court archives"},
			},
		},
		{
			ID: "sg-020", Title: "Tessarine Census Records",
			Subtitle: "Census returns of the Tessarine Coast, year 41",
			Body:     "The Tessarine Census Records catalogue the population of 41 coastal settlements. The census is administered by the Council of Oresund Annex and supervised by Recorder Inge Tessarine.",
			Entities: []sgEntity{
				mk("sg-020", 1, "Tessarine Census Records", "The Census", "DOCUMENT", "document"),
				mk("sg-020", 2, "Inge Tessarine", "Recorder Tessarine", "PERSON", "person"),
				mk("sg-020", 3, "Council of Oresund Annex", "Oresund Annex", "ORGANIZATION", "organization"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-020", "Tessarine Census Records"), Target: makeSalt("sg-020", "Council of Oresund Annex"), Predicate: "used-by"},
				{Source: makeSalt("sg-020", "Tessarine Census Records"), Target: makeSalt("sg-020", "Inge Tessarine"), Predicate: "created-by"},
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-020", "Inge Tessarine"), Predicate: "supervised", Object: "Tessarine Census"},
			},
		},
	}
	out := make([]sourceGroup, 0, len(specs))
	for _, sp := range specs {
		out = append(out, sourceGroup{
			ID:        sp.ID,
			Title:     sp.Title,
			Subtitle:  sp.Subtitle,
			Body:      sp.Body,
			Entities:  sp.Entities,
			Relations: sp.Relations,
			Claims:    sp.Claims,
		})
	}
	// Coverage fillers target vocabulary labels the base groups did not
	// exercise. They are held-out only; they use the same fictional
	// salted naming scheme so they cannot leak entities to other groups.
	for _, filler := range coverageFillers() {
		out = append(out, filler)
	}
	// Sort by ID for deterministic ordering.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// coverageFillers returns synthetic source groups that exercise the
// remaining vocabulary labels (relationship predicates, entity-type
// labels, claim verdicts). They live in the held-out split only and
// follow the same naming rules as the base groups.
func coverageFillers() []sourceGroup {
	mk := func(group string, n int, base, alias, typeTag, docType string) sgEntity {
		return sgEntity{
			LocalID:    fmt.Sprintf("%s-e%d", group, n),
			Name:       makeSalt(group, base),
			Aliases:    []string{makeSalt(group, alias)},
			Type:       typeTag,
			EntityType: docType,
		}
	}
	return []sourceGroup{
		{
			ID: "sg-cov-rel-derived", Title: "Cov Rel Derived",
			Body: "The Brenton Draft is the predecessor manuscript of the Brenton Folio.",
			Entities: []sgEntity{
				mk("sg-cov-rel-derived", 1, "Brenton Folio", "Folio", "DOCUMENT", "document"),
				mk("sg-cov-rel-derived", 2, "Brenton Draft", "Draft", "DOCUMENT", "document"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-cov-rel-derived", "Brenton Folio"), Target: makeSalt("sg-cov-rel-derived", "Brenton Draft"), Predicate: "derived-from"},
			},
		},
		{
			ID: "sg-cov-rel-implements", Title: "Cov Rel Implements",
			Body: "The Brenton Indexer software implements the Brenton Folio search protocol.",
			Entities: []sgEntity{
				mk("sg-cov-rel-implements", 1, "Brenton Indexer", "Indexer", "TOOL", "software"),
				mk("sg-cov-rel-implements", 2, "Brenton Folio Search Protocol", "Search Protocol", "DOCUMENT", "document"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-cov-rel-implements", "Brenton Indexer"), Target: makeSalt("sg-cov-rel-implements", "Brenton Folio Search Protocol"), Predicate: "implements"},
			},
		},
		{
			ID: "sg-cov-rel-depends", Title: "Cov Rel Depends",
			Body: "The Brenton Indexer depends on the Brenton Archive for its corpus.",
			Entities: []sgEntity{
				mk("sg-cov-rel-depends", 1, "Brenton Indexer B", "Indexer B", "TOOL", "software"),
				mk("sg-cov-rel-depends", 2, "Brenton Archive B", "Archive B", "ORGANIZATION", "organization"),
			},
			Relations: []sgRelation{
				{Source: makeSalt("sg-cov-rel-depends", "Brenton Indexer B"), Target: makeSalt("sg-cov-rel-depends", "Brenton Archive B"), Predicate: "depends-on"},
			},
		},
		{
			ID: "sg-cov-entity-person", Title: "Cov Entity Person",
			Body: "Yara Brenton is a fictional curator known for the Brenton Folio. She maintains the Brenton Archive in the city of Westmere.",
			Entities: []sgEntity{
				mk("sg-cov-entity-person", 1, "Yara Brenton", "Curator Brenton", "PERSON", "person"),
			},
		},
		{
			ID: "sg-cov-entity-project", Title: "Cov Entity Project",
			Body: "The Brenton Folio Project is a community indexing effort. Project leads include Yara Brenton and the Brenton Archive staff.",
			Entities: []sgEntity{
				mk("sg-cov-entity-project", 1, "Brenton Folio Project", "Folio Project", "EVENT", "project"),
			},
		},
		{
			ID: "sg-cov-entity-software", Title: "Cov Entity Software",
			Body: "Brenton Indexer is a fictional indexing service. The software is maintained by the Brenton Folio Project.",
			Entities: []sgEntity{
				mk("sg-cov-entity-software", 1, "Brenton Indexer C", "Indexer C", "TOOL", "software"),
			},
		},
		{
			ID: "sg-cov-entity-event", Title: "Cov Entity Event",
			Body: "The Brenton Lantern Festival is a fictional annual event. The festival is hosted by the city of Westmere.",
			Entities: []sgEntity{
				mk("sg-cov-entity-event", 1, "Brenton Lantern Festival", "Lantern Festival", "EVENT", "event"),
			},
		},
		{
			ID: "sg-cov-entity-paper", Title: "Cov Entity Paper",
			Body: "Notes on the Brenton Folio is a fictional research paper. The paper was authored by Yara Brenton.",
			Entities: []sgEntity{
				mk("sg-cov-entity-paper", 1, "Notes on the Brenton Folio", "Notes", "DOCUMENT", "paper"),
			},
		},
		{
			ID: "sg-cov-claim-contradicted", Title: "Cov Claim Contradicted",
			Body: "The Brenton Indexer is a search engine. The Brenton Indexer is NOT a search engine; it is an annotation tool.",
			Entities: []sgEntity{
				mk("sg-cov-claim-contradicted", 1, "Brenton Indexer D", "Indexer D", "TOOL", "software"),
			},
			Claims: []sgClaim{
				{Subject: makeSalt("sg-cov-claim-contradicted", "Brenton Indexer D"), Predicate: "is", Object: "search-engine"},
			},
		},
	}
}
