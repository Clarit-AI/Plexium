// Package candidateex runs the candidate-generation exercise required by
// KHA-579 protocol v0.3.
//
// IMPORTANT — this package measures SUPPLIED-POOL stress test recall,
// not realistic discovery recall. The pool passed to Run() is built by
// the caller; in the pilot harness the pool is the union of the
// fixture's own Candidates lists, which is gold-derived. The exercise
// is useful as a stress test (does candidate.Generate handle the
// supplied pool? does deliberate gold omission correctly crater recall
// for the omitted group?) and the deliberate-omission counterexample
// proves the measurement is honest, but the headline recall numbers
// do not generalize to discovery. Real discovery recall lives in
// the discovery package; see discovery.Report.
//
// Diagnostics the exercise supports:
//
//   - Gold omission: a fixture deliberately removes the gold entity from
//     the pool; the recall measurement must reflect the miss.
//   - Pool truncation: a fixture caps the pool at MaxEntities; the
//     truncation flag must be set on the shortlist.
//   - Per-source-group independence: each group runs its own generation
//     against its own excerpts; no global pool is shared.
package candidateex

import (
	"errors"
	"sort"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/candidate"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

// Result is the outcome of one candidate-generation exercise for a fixture.
type Result struct {
	FixtureID        string        `json:"fixtureId"`
	Task             protocol.Task `json:"task"`
	SourceGroup      string        `json:"sourceGroup"`
	GeneratedCount   int           `json:"generatedCount"`
	Truncated        bool          `json:"truncated"`
	GoldEntityTitle  string        `json:"goldEntityTitle,omitempty"`
	GoldEdgeSourceID string        `json:"goldEdgeSourceId,omitempty"`
	GoldEdgeTargetID string        `json:"goldEdgeTargetId,omitempty"`
	GoldInPool       bool          `json:"goldInPool"`
	EntityRecall     float64       `json:"entityRecall"`
	EdgeRecall       float64       `json:"edgeRecall"`
	PoolSize         int           `json:"poolSize"`
	TruncationNote   string        `json:"truncationNote,omitempty"`
	GenerationNs     int64         `json:"generationNs"`
}

// Report aggregates all candidate-generation results.
//
// NOTE: this report measures supplied-pool stress-test recall, not
// realistic discovery recall. The Provenance and Notes fields make
// that distinction explicit in the JSON output.
type Report struct {
	Kind                string                     `json:"kind"` // "supplied-pool-stress-test"
	Provenance          string                     `json:"provenance"`
	Notes               string                     `json:"notes"`
	FixtureCount        int                        `json:"fixtureCount"`
	BySourceGroup       map[string]SourceGroupStat `json:"bySourceGroup"`
	Overall             OverallStat                `json:"overall"`
	OmittedGoldFixtures []string                   `json:"omittedGoldFixtures,omitempty"`
	TruncatedFixtures   []string                   `json:"truncatedFixtures,omitempty"`
}

// SourceGroupStat is the per-source-group rollup.
type SourceGroupStat struct {
	FixtureCount int     `json:"fixtureCount"`
	EntityRecall float64 `json:"entityRecall"`
	EdgeRecall   float64 `json:"edgeRecall"`
}

// OverallStat is the cross-group rollup.
type OverallStat struct {
	EntityRecall float64 `json:"entityRecall"`
	EdgeRecall   float64 `json:"edgeRecall"`
}

// PoolEntry is one entry in the candidate pool. The exercise supplies a
// pool per source group; the harness generates a shortlist against the
// fixture's excerpts and reports recall against the gold.
//
// ID is the opaque identifier preserved on the generated shortlist. When
// ID is non-empty, candidate.Generate emits it verbatim; classifier
// inputs see only opaque tokens. When ID is empty, candidate.Generate
// falls back to "role:index" form (the ranker's matched tag + pool
// index).
type PoolEntry struct {
	ID      string
	Index   int
	Title   string
	Aliases []string
}

// RunOptions configures one exercise invocation. DeliberateGoldOmit
// excludes a specific gold entity from the pool so the recall
// measurement reflects a true miss rather than a free win.
type RunOptions struct {
	// Pool returns the candidate pool for the supplied source group. The
	// pool is supplied by the caller so the harness does not assume the
	// fixture's Candidates field is the only source of truth.
	Pool func(group string) []PoolEntry
	// GoldEntities returns the gold entity titles for the supplied source
	// group. The entity recall is measured against these.
	GoldEntities func(group string) []string
	// GoldEdges returns the gold directed edges (source ID, target ID)
	// for the supplied source group. The edge recall is measured against
	// these. When nil, edge recall is 0 for that group.
	GoldEdges func(group string) []candidate.DirectedEdgeCandidate
	// DeliberateGoldOmit, when non-empty, removes the named entity from
	// the pool before generation. The fixture's GoldEntityTitle is set to
	// this name so the report explicitly records the omission.
	DeliberateGoldOmit func(group string) string
}

// Run executes the exercise against the supplied fixtures. Each fixture
// must have a Task of Relationship or ClaimSupport (entity-type tasks do
// not exercise candidate generation in this corpus).
func Run(fixtures []protocol.Fixture, opts RunOptions) (*Report, error) {
	if opts.Pool == nil {
		return nil, errors.New("candidateex: Pool function is required")
	}
	if opts.GoldEntities == nil {
		return nil, errors.New("candidateex: GoldEntities function is required")
	}
	rep := &Report{
		Kind:          "supplied-pool-stress-test",
		Provenance:    "pool built from caller-supplied poolFn; in the pilot harness the pool is the union of the fixture's Candidates list (gold-derived). This report measures SUPPLIED-POOL STRESS-TEST recall only, not realistic discovery recall.",
		Notes:         "realistic discovery recall lives in the discovery package; see discovery.Report for the evidence-only deterministic baseline.",
		FixtureCount:  len(fixtures),
		BySourceGroup: map[string]SourceGroupStat{},
	}
	entityRecallSum := 0.0
	edgeRecallSum := 0.0
	entityRecallCount := 0
	edgeRecallCount := 0
	for _, f := range fixtures {
		poolEntries := opts.Pool(f.SourceGroup)
		// Deliberate gold omission.
		if opts.DeliberateGoldOmit != nil {
			if omit := opts.DeliberateGoldOmit(f.SourceGroup); omit != "" {
				poolEntries = filterOut(poolEntries, omit)
			}
		}
		pool := candidate.Pool{}
		for _, e := range poolEntries {
			pool = append(pool, candidate.PoolEntry{ID: e.ID, Index: e.Index, Title: e.Title, Aliases: e.Aliases})
		}
		goldEntities := opts.GoldEntities(f.SourceGroup)
		var goldEdges []candidate.DirectedEdgeCandidate
		if opts.GoldEdges != nil {
			goldEdges = opts.GoldEdges(f.SourceGroup)
		}
		res := Result{
			FixtureID:   f.ID,
			Task:        f.Task,
			SourceGroup: f.SourceGroup,
			PoolSize:    len(pool),
		}
		// Run candidate.Generate per task.
		switch f.Task {
		case protocol.TaskEntityType, protocol.TaskCandidateType, protocol.TaskClaimSupport, protocol.TaskRelationship:
			start := time.Now()
			list := candidate.Generate(f.Task, pool, f.Excerpts)
			res.GenerationNs = time.Since(start).Nanoseconds()
			res.GeneratedCount = len(list.Candidates)
			res.Truncated = list.Truncated
			res.TruncationNote = list.TruncationNote
			if len(goldEntities) > 0 {
				eRecall, _ := candidate.Recall(list, goldEntities, nil)
				res.EntityRecall = eRecall
				// Did the gold entity land in the pool?
				res.GoldEntityTitle = goldEntities[0]
				res.GoldInPool = false
				for _, p := range pool {
					if p.Title == goldEntities[0] {
						res.GoldInPool = true
					}
				}
			}
			if len(goldEdges) > 0 {
				_, edgeR := candidate.Recall(list, nil, goldEdges)
				res.EdgeRecall = edgeR
				if len(goldEdges) > 0 {
					res.GoldEdgeSourceID = goldEdges[0].SourceID
					res.GoldEdgeTargetID = goldEdges[0].TargetID
				}
			}
			// Per-source-group rollup.
			sg := rep.BySourceGroup[f.SourceGroup]
			sg.FixtureCount++
			if res.EntityRecall > 0 {
				sg.EntityRecall += res.EntityRecall
			}
			if res.EdgeRecall > 0 {
				sg.EdgeRecall += res.EdgeRecall
			}
			rep.BySourceGroup[f.SourceGroup] = sg
			entityRecallSum += res.EntityRecall
			entityRecallCount++
			if len(goldEdges) > 0 {
				edgeRecallSum += res.EdgeRecall
				edgeRecallCount++
			}
		}
		// Track omitted-gold / truncated fixtures.
		if opts.DeliberateGoldOmit != nil && opts.DeliberateGoldOmit(f.SourceGroup) != "" {
			rep.OmittedGoldFixtures = append(rep.OmittedGoldFixtures, f.ID)
		}
		if res.Truncated {
			rep.TruncatedFixtures = append(rep.TruncatedFixtures, f.ID)
		}
	}
	// Finalize per-group averages.
	for k, sg := range rep.BySourceGroup {
		if sg.FixtureCount > 0 {
			if sg.EntityRecall > 0 {
				sg.EntityRecall = sg.EntityRecall / float64(sg.FixtureCount)
			}
			if sg.EdgeRecall > 0 {
				sg.EdgeRecall = sg.EdgeRecall / float64(sg.FixtureCount)
			}
			rep.BySourceGroup[k] = sg
		}
	}
	if entityRecallCount > 0 {
		rep.Overall.EntityRecall = entityRecallSum / float64(entityRecallCount)
	}
	if edgeRecallCount > 0 {
		rep.Overall.EdgeRecall = edgeRecallSum / float64(edgeRecallCount)
	}
	sort.Strings(rep.OmittedGoldFixtures)
	sort.Strings(rep.TruncatedFixtures)
	return rep, nil
}

func filterOut(entries []PoolEntry, title string) []PoolEntry {
	out := make([]PoolEntry, 0, len(entries))
	for _, e := range entries {
		if e.Title == title {
			continue
		}
		out = append(out, e)
	}
	return out
}
