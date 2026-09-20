// Package discovery provides an evidence-only deterministic baseline
// for candidate generation. It extracts candidate entities from raw
// markdown source text using three patterns:
//
//   - explicit markdown headings (lines starting with "# " through "###### ")
//   - explicit wikilink syntax ([[Title]] or [[Title|alias]])
//   - explicit definition syntax ("def: <Title> = <description>")
//
// The baseline never reads fixture Candidates, ExpectedLabel, or
// GoldEntities. The scorer joins the discovered candidates against the
// gold only at scoring time.
//
// The baseline is intentionally narrow: it does NOT attempt to recover
// entities from natural prose. The harness records which entities the
// baseline MISSED so reviewers can see what real discovery would have
// to add. The metric is named "discovery recall" and the report makes
// clear it is a low-recall upper bound on what an entity-aware
// extractor might achieve.
package discovery

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/candidate"
)

// Source is a single source of raw markdown text the baseline extracts
// candidates from. The harness maps source groups to Sources without
// consulting fixture Candidates or ExpectedLabel.
type Source struct {
	Group string
	// Body is the raw markdown text the baseline reads.
	Body string
}

// PoolEntry is a discovered candidate with its ranker role tag and the
// line offset where the regex matched. The baseline returns entries in
// extraction order; the harness applies any additional ranking.
type PoolEntry struct {
	Title   string
	Alias   string
	Pattern string // "heading" | "wikilink" | "definition"
	Offset  int
}

// Extract runs the deterministic discovery baseline over the supplied
// sources. The returned map keys on Source.Group.
func Extract(sources []Source) map[string][]PoolEntry {
	out := map[string][]PoolEntry{}
	for _, s := range sources {
		out[s.Group] = extractOne(s.Body)
	}
	return out
}

var (
	headingRe    = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*$`)
	wikilinkRe   = regexp.MustCompile(`\[\[([^\]|]+?)(?:\|([^\]]+?))?\]\]`)
	definitionRe = regexp.MustCompile(`(?m)^def:\s+([^=]+?)\s*=\s*(.+)$`)
)

func extractOne(body string) []PoolEntry {
	var out []PoolEntry
	seen := map[string]bool{}
	for _, m := range headingRe.FindAllStringSubmatchIndex(body, -1) {
		title := strings.TrimSpace(body[m[2]:m[3]])
		if title == "" {
			continue
		}
		if seen[strings.ToLower(title)] {
			continue
		}
		seen[strings.ToLower(title)] = true
		out = append(out, PoolEntry{Title: title, Pattern: "heading", Offset: m[0]})
	}
	for _, m := range wikilinkRe.FindAllStringSubmatchIndex(body, -1) {
		title := strings.TrimSpace(body[m[2]:m[3]])
		alias := ""
		if m[4] != -1 {
			alias = strings.TrimSpace(body[m[4]:m[5]])
		}
		if title == "" {
			continue
		}
		if seen[strings.ToLower(title)] {
			continue
		}
		seen[strings.ToLower(title)] = true
		out = append(out, PoolEntry{Title: title, Alias: alias, Pattern: "wikilink", Offset: m[0]})
	}
	for _, m := range definitionRe.FindAllStringSubmatchIndex(body, -1) {
		title := strings.TrimSpace(body[m[2]:m[3]])
		if title == "" {
			continue
		}
		if seen[strings.ToLower(title)] {
			continue
		}
		seen[strings.ToLower(title)] = true
		out = append(out, PoolEntry{Title: title, Pattern: "definition", Offset: m[0]})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Offset < out[j].Offset })
	return out
}

// JoinGold maps the discovered entries against a per-group gold pool
// the caller supplies separately from the discovered pool. The gold
// pool is intentionally NOT derived from the discovered entries.
//
// groups is the explicit list of source-group IDs to score. The helper
// uses it because the gold and edges are functions that cannot be
// iterated directly.
func JoinGold(discovered map[string][]PoolEntry, groups []string, gold func(group string) []string, edges func(group string) []candidate.DirectedEdgeCandidate) (entityRecall float64, edgeRecall float64, missedEntities []string, missedEdges []candidate.DirectedEdgeCandidate) {
	var sumEntity, sumEdge float64
	var nEntity, nEdge int
	for _, group := range groups {
		goldList := gold(group)
		discoveredList := discovered[group]
		discoveredTitles := map[string]bool{}
		for _, d := range discoveredList {
			discoveredTitles[strings.ToLower(d.Title)] = true
		}
		var groupHits int
		for _, g := range goldList {
			if discoveredTitles[strings.ToLower(g)] {
				groupHits++
			} else {
				missedEntities = append(missedEntities, g)
			}
		}
		if len(goldList) > 0 {
			sumEntity += float64(groupHits) / float64(len(goldList))
			nEntity++
		}
		if edges != nil {
			goldEdges := edges(group)
			var edgeHits int
			for _, e := range goldEdges {
				if discoveredTitles[strings.ToLower(e.SourceID)] || discoveredTitles[strings.ToLower(e.TargetID)] {
					edgeHits++
				} else {
					missedEdges = append(missedEdges, e)
				}
			}
			if len(goldEdges) > 0 {
				sumEdge += float64(edgeHits) / float64(len(goldEdges))
				nEdge++
			}
		}
	}
	if nEntity > 0 {
		entityRecall = sumEntity / float64(nEntity)
	}
	if nEdge > 0 {
		edgeRecall = sumEdge / float64(nEdge)
	}
	return
}

// Report is the discovery-baseline summary. The baseline is intentionally
// narrow; the report records what it MISSED as much as what it FOUND so
// the gap between "discovery-by-markdown-pattern" and "discovery by
// reading the body" is visible.
type Report struct {
	Kind             string                            `json:"kind"` // "evidence-only-discovery-baseline"
	Provenance       string                            `json:"provenance"`
	Notes            string                            `json:"notes"`
	SourceCount      int                               `json:"sourceCount"`
	DiscoveredCount  int                               `json:"discoveredCount"`
	EntityRecall     float64                           `json:"entityRecall"`
	EdgeRecall       float64                           `json:"edgeRecall"`
	MissedEntities   []string                          `json:"missedEntities,omitempty"`
	MissedEdges      []candidate.DirectedEdgeCandidate `json:"missedEdges,omitempty"`
	GenerationTimeNs int64                             `json:"generationNs"`
	ByPattern        map[string]int                    `json:"byPattern"`
}

// Run executes the discovery baseline against the supplied sources and
// gold, then reports what the baseline found and missed.
func Run(sources []Source, gold func(group string) []string, edges func(group string) []candidate.DirectedEdgeCandidate, allGroups []string) *Report {
	start := time.Now()
	discovered := Extract(sources)
	entityRecall, edgeRecall, missedE, missedEdges := JoinGold(discovered, allGroups, gold, edges)
	rep := &Report{
		Kind:             "evidence-only-discovery-baseline",
		Provenance:       "candidates derived from raw markdown body via three regex patterns (heading, wikilink, definition). No fixture-side data is consulted; the scorer joins discovered candidates against the gold at scoring time.",
		Notes:            "this baseline is intentionally narrow; the missedEntities/missedEdges lists show what real discovery would have to add.",
		SourceCount:      len(sources),
		EntityRecall:     entityRecall,
		EdgeRecall:       edgeRecall,
		MissedEntities:   missedE,
		MissedEdges:      missedEdges,
		GenerationTimeNs: time.Since(start).Nanoseconds(),
		ByPattern:        map[string]int{},
	}
	for _, list := range discovered {
		for _, d := range list {
			rep.DiscoveredCount++
			rep.ByPattern[d.Pattern]++
		}
	}
	return rep
}
