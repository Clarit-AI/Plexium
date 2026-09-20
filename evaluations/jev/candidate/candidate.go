// Package candidate implements deterministic candidate generation for the
// Jev harness. It produces shortlist entries from a candidate pool using
// title / alias / wikilink matching and co-occurrence within an evidence
// passage, with stable tie-breaking and bounded truncation.
//
// The rules follow KHA-579 protocol v0.1:
//
//   - Title / alias / wikilink matches are highest priority, in that order.
//   - Co-occurrence within an evidence passage is the secondary signal.
//   - The maximum is 12 entities (entity-type task) and 24 directed edge
//     candidates (relationship task); truncation is recorded.
//   - A missing gold entity / edge counts as a candidate miss even when the
//     downstream classifier is perfect.
package candidate

import (
	"sort"
	"strings"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

// MaxEntities is the cap on candidates produced for entity/document-type
// tasks. Protocol v0.1 sets it at 12.
const MaxEntities = 12

// MaxDirectedEdgeCandidates is the cap on candidates produced for
// relationship tasks. Protocol v0.1 sets it at 24.
const MaxDirectedEdgeCandidates = 24

// PoolEntry is one entry in the candidate pool a source-group contributes.
// Indexes are stable per source-group so candidate IDs survive across runs.
type PoolEntry struct {
	Index    int
	Title    string
	Aliases  []string
	Wikilink string
}

// Pool is a deterministic, sorted view of all candidate pool entries for a
// fixture's source group.
type Pool []PoolEntry

// Index returns the candidate's stable ID, the prefix being the role tag so
// downstream reviewers can tell at a glance how a candidate was surfaced.
func CandidateID(role string, idx int) string {
	return role + ":" + itoa(idx)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	negative := false
	if i < 0 {
		negative = true
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if negative {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

// Shortlist is the result of running Generate over a fixture's evidence and
// pool. It carries the candidates in their final ranking plus metadata
// required to score candidate recall.
type Shortlist struct {
	Candidates     []protocol.Candidate
	Truncated      bool
	TruncationNote string
	GeneratedBy    string
}

// Generate produces a candidate shortlist for the supplied task. The
// truncation flag indicates whether the pool was bounded.
//
// The procedure:
//
//  1. For each PoolEntry, compute a match score against each evidence excerpt.
//     Title > alias > wikilink > none. The first evidence excerpt that
//     produces any match wins; later excerpts do not promote a candidate.
//  2. Co-occurrence within an excerpt counts as a secondary signal when no
//     direct title / alias / wikilink match exists.
//  3. Candidates are ranked by role priority (title > alias > wikilink >
//     co-occurrence), then by the originating PoolEntry.Index for stable
//     tie-breaking.
//  4. Truncate to MaxEntities (entity-type) or MaxDirectedEdgeCandidates
//     (relationship). For relationship tasks, the recorded TruncationNote
//     describes the cap.
func Generate(task protocol.Task, pool Pool, excerpts []protocol.Excerpt) Shortlist {
	scored := scorePool(pool, excerpts)
	switch task {
	case protocol.TaskEntityType:
		return truncateAndWrap(scored, MaxEntities, "entity-type cap 12")
	case protocol.TaskRelationship:
		return truncateAndWrap(scored, MaxDirectedEdgeCandidates, "directed-edge cap 24")
	default:
		return truncateAndWrap(scored, MaxDirectedEdgeCandidates, "default cap 24")
	}
}

type scoredCandidate struct {
	entry   PoolEntry
	role    string
	rank    int
	cooccur bool
}

func scorePool(pool Pool, excerpts []protocol.Excerpt) []scoredCandidate {
	haystack := make([]string, len(excerpts))
	for i, e := range excerpts {
		haystack[i] = strings.ToLower(e.Text)
	}
	scored := make([]scoredCandidate, 0, len(pool))
	for _, p := range pool {
		best := ""
		rank := 4
		cooccur := false
		for _, h := range haystack {
			title := strings.ToLower(strings.TrimSpace(p.Title))
			if title != "" && strings.Contains(h, title) {
				best = "title"
				rank = 0
				break
			}
			for _, alias := range p.Aliases {
				alias = strings.ToLower(strings.TrimSpace(alias))
				if alias != "" && strings.Contains(h, alias) {
					best = "alias"
					if rank > 1 {
						rank = 1
					}
					break
				}
			}
			if best != "" {
				break
			}
			link := strings.ToLower(strings.TrimSpace(p.Wikilink))
			if link != "" && strings.Contains(h, link) {
				best = "wikilink"
				if rank > 2 {
					rank = 2
				}
				break
			}
			if !cooccur && (titleMatchCooccur(p.Title, h) || aliasMatchCooccur(p.Aliases, h)) {
				cooccur = true
			}
		}
		if best == "" && cooccur {
			best = "unknown"
			rank = 3
		}
		if best == "" {
			continue
		}
		scored = append(scored, scoredCandidate{entry: p, role: best, rank: rank, cooccur: cooccur})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].rank != scored[j].rank {
			return scored[i].rank < scored[j].rank
		}
		return scored[i].entry.Index < scored[j].entry.Index
	})
	return scored
}

func titleMatchCooccur(title, haystack string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	if t == "" {
		return false
	}
	words := strings.Fields(t)
	if len(words) == 0 {
		return false
	}
	matched := 0
	for _, w := range words {
		if len(w) < 4 {
			continue
		}
		if strings.Contains(haystack, w) {
			matched++
		}
	}
	return matched > 0 && matched >= len(words)/2
}

func aliasMatchCooccur(aliases []string, haystack string) bool {
	for _, a := range aliases {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" || len(a) < 4 {
			continue
		}
		if strings.Contains(haystack, a) {
			return true
		}
	}
	return false
}

func truncateAndWrap(scored []scoredCandidate, cap int, note string) Shortlist {
	truncated := false
	if len(scored) > cap {
		scored = scored[:cap]
		truncated = true
	}
	out := Shortlist{
		Candidates:     make([]protocol.Candidate, 0, len(scored)),
		Truncated:      truncated,
		TruncationNote: note,
		GeneratedBy:    "deterministic-title-alias-wikilink",
	}
	for _, s := range scored {
		out.Candidates = append(out.Candidates, protocol.Candidate{
			ID:    CandidateID(s.role, s.entry.Index),
			Title: s.entry.Title,
			Alias: firstAlias(s.entry),
			Role:  s.role,
		})
	}
	return out
}

func firstAlias(p PoolEntry) string {
	for _, a := range p.Aliases {
		a = strings.TrimSpace(a)
		if a != "" {
			return a
		}
	}
	return ""
}

// DirectedEdgeCandidate pairs a source and a target candidate ID. The
// harness uses these for relationship tasks. Source / target ordering is
// preserved by the caller.
type DirectedEdgeCandidate struct {
	SourceID string
	TargetID string
}

// DirectedEdges enumerates a deterministic cartesian product of the leading
// candidates, capped at MaxDirectedEdgeCandidates. The order is stable across
// runs because the input Shortlist is already ranked.
func DirectedEdges(list Shortlist, max int) []DirectedEdgeCandidate {
	if max <= 0 || max > MaxDirectedEdgeCandidates {
		max = MaxDirectedEdgeCandidates
	}
	out := make([]DirectedEdgeCandidate, 0, max)
	for i := range list.Candidates {
		if len(out) >= max {
			break
		}
		for j := range list.Candidates {
			if len(out) >= max {
				break
			}
			if i == j {
				continue
			}
			out = append(out, DirectedEdgeCandidate{
				SourceID: list.Candidates[i].ID,
				TargetID: list.Candidates[j].ID,
			})
		}
	}
	return out
}

// Recall measures the fraction of gold entities/edges the shortlist contains.
// gold is a list of titles (entity task) or source|target pairs (relationship
// task). For entity-task recall the candidate's title or alias matches.
// For relationship-task recall the candidate's ID pair is matched.
func Recall(list Shortlist, gold []string, goldEdges []DirectedEdgeCandidate) (entityRecall float64, edgeRecall float64) {
	if len(gold) == 0 && len(goldEdges) == 0 {
		return 0, 0
	}
	if len(gold) > 0 {
		matched := 0
		titles := make(map[string]struct{}, len(list.Candidates))
		aliases := make(map[string]struct{}, len(list.Candidates))
		for _, c := range list.Candidates {
			titles[strings.ToLower(c.Title)] = struct{}{}
			if c.Alias != "" {
				aliases[strings.ToLower(c.Alias)] = struct{}{}
			}
		}
		for _, g := range gold {
			g = strings.ToLower(strings.TrimSpace(g))
			if g == "" {
				continue
			}
			if _, ok := titles[g]; ok {
				matched++
				continue
			}
			if _, ok := aliases[g]; ok {
				matched++
			}
		}
		entityRecall = float64(matched) / float64(len(gold))
	}
	if len(goldEdges) > 0 {
		matched := 0
		set := make(map[string]struct{}, len(list.Candidates)*len(list.Candidates))
		for i := range list.Candidates {
			for j := range list.Candidates {
				if i == j {
					continue
				}
				set[list.Candidates[i].ID+"|"+list.Candidates[j].ID] = struct{}{}
			}
		}
		for _, e := range goldEdges {
			if _, ok := set[e.SourceID+"|"+e.TargetID]; ok {
				matched++
			}
		}
		edgeRecall = float64(matched) / float64(len(goldEdges))
	}
	return
}
