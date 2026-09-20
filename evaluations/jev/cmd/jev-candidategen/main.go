// Command jev-candidategen runs the candidate-generation exercise. It
// invokes candidate.Generate against a curated set of "free" fixtures
// (those whose CandidateGeneration field is the deterministic
// ranker's exercise path, not the supplied-candidate path). It measures
// recall against the gold pool and reports honest miss counts. It is
// the only place the harness measures candidate-generation quality
// without pre-supplied candidate lists.
//
// Usage:
//
//	go run ./cmd/jev-candidategen -fixtures pilot/fixtures.jsonl \
//	    -manifest pilot/fixtures.manifest.json \
//	    -deliberate-omit sg-001
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Clarit-AI/Plexium/evaluations/jev/candidate"
	"github.com/Clarit-AI/Plexium/evaluations/jev/candidateex"
	"github.com/Clarit-AI/Plexium/evaluations/jev/loader"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

func main() {
	var (
		fixturesPath = flag.String("fixtures", "pilot/fixtures.jsonl", "JSONL fixtures")
		manifestPath = flag.String("manifest", "pilot/fixtures.manifest.json", "manifest")
		deliberate   = flag.String("deliberate-omit", "", "source group ID whose gold entity to omit from the pool")
		out          = flag.String("out", "pilot/candidate-report.json", "output report")
	)
	flag.Parse()
	loaded, err := loader.Load(*fixturesPath, *manifestPath)
	if err != nil {
		die("load: %v", err)
	}
	if err := loader.ValidateFixtures(loaded.Fixtures); err != nil {
		die("validate: %v", err)
	}
	// Build per-source-group pool entries. The pool is the union of
	// entity titles across the group's relationship / claim-support
	// fixtures, with the entity's LocalID carried as the opaque ID. The
	// harness deliberately does NOT augment the pool from the
	// entity-type-base case, which has Candidates=nil: doing so would
	// silently gold-feed the exercise.
	type poolKey struct{}
	_ = poolKey{}
	poolFn := func(group string) []candidateex.PoolEntry {
		// Take the union of opaque IDs + titles from the group's
		// relationship / claim-support fixtures.
		seen := map[string]int{}
		var out []candidateex.PoolEntry
		for _, f := range loaded.Fixtures {
			if f.SourceGroup != group {
				continue
			}
			if f.Task == protocol.TaskEntityType {
				continue
			}
			for _, c := range f.Candidates {
				key := c.ID
				if key == "" {
					key = c.Title
				}
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = len(out)
				out = append(out, candidateex.PoolEntry{
					ID:      c.ID,
					Index:   len(out),
					Title:   c.Title,
					Aliases: []string{c.Alias},
				})
			}
		}
		return out
	}
	goldEntitiesFn := func(group string) []string {
		// The first candidate title from any group fixture with
		// Candidates populated is the gold entity title. Without an
		// entity-type candidate to anchor this, the gold is "unknown"
		// and the exercise skips.
		for _, f := range loaded.Fixtures {
			if f.SourceGroup != group {
				continue
			}
			if len(f.Candidates) > 0 {
				return []string{f.Candidates[0].Title}
			}
		}
		return nil
	}
	goldEdgesFn := func(group string) []candidate.DirectedEdgeCandidate {
		for _, f := range loaded.Fixtures {
			if f.SourceGroup != group {
				continue
			}
			if f.Task != protocol.TaskRelationship {
				continue
			}
			if f.EdgeSourceID == "" || f.EdgeTargetID == "" {
				continue
			}
			return []candidate.DirectedEdgeCandidate{{SourceID: f.EdgeSourceID, TargetID: f.EdgeTargetID}}
		}
		return nil
	}
	deliberateFn := func(group string) string {
		if *deliberate != "" && group == *deliberate {
			ents := goldEntitiesFn(group)
			if len(ents) > 0 {
				return ents[0]
			}
		}
		return ""
	}
	// Exercise fixtures: those that need candidate generation, i.e.
	// relationship and claim-support fixtures with at least one excerpt
	// (skip the explicit missing-evidence fixtures).
	var exFixtures []protocol.Fixture
	for _, f := range loaded.Fixtures {
		if f.Task != protocol.TaskRelationship && f.Task != protocol.TaskClaimSupport {
			continue
		}
		if len(f.Excerpts) == 0 {
			continue
		}
		exFixtures = append(exFixtures, f)
	}
	rep, err := candidateex.Run(exFixtures, candidateex.RunOptions{
		Pool:               poolFn,
		GoldEntities:       goldEntitiesFn,
		GoldEdges:          goldEdgesFn,
		DeliberateGoldOmit: deliberateFn,
	})
	if err != nil {
		die("candidateex: %v", err)
	}
	f, err := os.Create(*out)
	if err != nil {
		die("create: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		die("encode: %v", err)
	}
	fmt.Fprintf(os.Stderr, "candidate report: entity-recall=%.3f edge-recall=%.3f omitted=%d truncated=%d\n",
		rep.Overall.EntityRecall, rep.Overall.EdgeRecall, len(rep.OmittedGoldFixtures), len(rep.TruncatedFixtures))
	_ = strings.Contains
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "jev-candidategen: "+format+"\n", args...)
	os.Exit(1)
}
