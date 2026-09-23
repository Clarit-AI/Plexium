# KHA-579 v0.4 study corpus — awaiting human adjudication

This directory contains a new offline study corpus built for the frozen v0.4
evaluation conventions. It is separate from both the legacy 332-case smoke
corpus and the 24-case human-approved tuning packet.

**No quality result exists for this corpus.** All 720 proposed labels are
agent-authored, carry `reviewStatus: "unreviewed"`, and have no reviewer. A
human must adjudicate the fixtures before they can be scored as gold. The
author string, `agent:codex-gpt-5.6-sol-9ed7c23e`, identifies the actual
authoring agent and harness rather than implying human authorship.

## Construction and sample-size targets

The corpus uses 180 fictional source groups:

- 30 tuning groups, each contributing exactly one fixture for each of the
  four tasks (120 tuning fixtures; 30 per task).
- 150 held-out groups, each contributing one directed-relationship negative,
  one claim-support negative, and matched straightforward-positive controls
  for those two tasks (600 held-out fixtures).

Each held-out negative task has 150 cases from 150 distinct source-group IDs.
That is a row and provenance-group count, **not 150 independent trials**. The
relationship negatives reduce to two normalized construction families
(explicit exclusion and equal-authority transfer conflict), and the claim
negatives reduce to two (certified-total contradiction and equal-authority
audit conflict). Distinct fictional names and group IDs do not increase that
construction-cluster count. Point rates may use 150 cases as their denominator,
but uncertainty cannot treat them as 150 independent Bernoulli observations;
the construction family is the defensible cluster unit, and two clusters per
negative task are inadequate for a stable cluster-based interval.

| Task | Tuning | Held-out positive | Held-out negative | Total |
| --- | ---: | ---: | ---: | ---: |
| Document type (`entity-type`) | 30 | 0 | 0 | 30 |
| Candidate type | 30 | 0 | 0 | 30 |
| Directed relationship | 30 | 150 | 150 | 330 |
| Claim support | 30 | 150 | 150 | 330 |
| **Total** | **120** | **300** | **300** | **720** |

Held-out relationship negatives are 75 `no-supported-relationship` and 75
`insufficient-evidence`. Held-out claim negatives are 50 `contradicted` and
100 `insufficient-evidence`; the 50 contradicted cases provide a meaningful
denominator for contradiction-miss measurement once humans approve them.

## Frozen semantic conventions

The proposed rationales apply the approved v0.4 rules:

- document type follows the primary subject; missing or genuinely balanced
  subjects use `insufficient-evidence`;
- candidate typing uses its own canonical semantic-type vocabulary and also
  abstains on unresolved referents;
- `used-by` means the source is explicitly used by the target;
- association without explicit functional use is `related-to`;
- an artifact related to an organization's design, specification, readings,
  or holdings is `related-to` when that distinct object—not the organization
  itself—is the narrow predicate endpoint;
- relationship `insufficient-evidence` requires unresolved ambiguity or
  equal-authority conflict;
- `no-supported-relationship` is used only when the requested endpoints are
  explicitly negated or exhaustively absent;
- claim `contradicted` requires incompatible evidence, not an assumption that
  an incomplete source is exhaustive; and
- embedded instructions are treated as untrusted passage content.

Every record supplies both `rationale` and `rationaleEvidence` for human
review. Candidate lists are declared honestly as hand-authored frozen
shortlists; this corpus makes no candidate-discovery recall claim.

## Structural split isolation

Tuning and held-out use separate lexical banks and visibly different writing
constructions: tuning uses archival notes, referent briefs, correspondence
indexes, and archive cross-checks; held-out uses exclusion/provenance dockets,
audit leaves, equipment ledgers, and completion dockets. Their template-family
IDs are split-specific.

`loader.VerifySplitIndependence` checks all of the following across splits:

1. source-group IDs are disjoint;
2. template-family IDs are disjoint;
3. normalized candidate titles and aliases are disjoint; and
4. body-text entity sets are disjoint.

Most study bodies mark named entities as `[[Title]]` or `[[Title|alias]]`.
Thirty held-out source groups (120 fixtures) deliberately use ordinary,
unmarked body text; the other 480 held-out fixtures retain markup. Production
evidence can contain wikilinks, but this packet does not assume every entity is
universally annotated or alias-resolved. The mixed representation measures
classification from supplied candidates under both marked and unmarked
evidence, not open-ended entity discovery from arbitrary prose.

The loader checks explicit links, a conservative legacy title-case extractor,
and case-insensitive occurrences of every known candidate title or alias in
body text. It compares candidate-to-candidate, body-to-body, and cross-channel
candidate-to-body identities across splits. Regressions cover the old v0.3
“Vornholt Pass” leak, candidate-to-body reuse, and a bare lower-case “grey
cloak workshop” case variant. This remains a bounded lexical check: it does
not resolve unknown synonyms, morphological aliases, or unregistered
single-word references.

These checks establish **structural disjointness only**. They do not prove
statistical independence, ecological validity, absence of broader stylistic
correlation, or representativeness of production documents. Names are
synthetic and systematically composed from split-specific lexical banks; the
held-out construction is intentionally auditable, not a sample of natural
traffic.

## Challenge counts

Counts below are fixture memberships; a fixture may carry several challenges.

| Challenge | Entity type | Candidate type | Relationship | Claim support | Total |
| --- | ---: | ---: | ---: | ---: | ---: |
| straightforward-positive | 28 | 0 | 144 | 160 | 332 |
| straightforward-negative | 0 | 0 | 156 | 0 | 156 |
| missing-evidence | 2 | 3 | 0 | 0 | 5 |
| competing-candidates | 2 | 0 | 0 | 0 | 2 |
| negation | 0 | 0 | 78 | 0 | 78 |
| reversed-direction | 0 | 0 | 30 | 0 | 30 |
| rename | 0 | 0 | 180 | 0 | 180 |
| conflicting-sources | 0 | 0 | 78 | 110 | 188 |
| misleading-numbers-dates | 28 | 0 | 0 | 170 | 198 |
| irrelevant-context | 0 | 30 | 0 | 170 | 200 |
| invalidating-edit | 0 | 0 | 0 | 60 | 60 |
| adversarial-instruction | 0 | 0 | 0 | 180 | 180 |

Across held-out source groups, negative fixtures exercise renaming, explicit
absence or unresolved conflict, irrelevant numerical context, and embedded
adversarial instructions. Thirty relationship controls are genuine reverse
edges: evidence states target→source `depends-on`, while the question asks
source→target; their proposed `related-to` label preserves the established
association without inventing the reversed narrow predicate. The other
controls provide straightforward positives. Challenge tags describe the
actual evidence construction; they are not claims of model robustness.

The repeated constructions are intentionally visible limitations. The 50
contradicted rows contain genuinely incompatible certified totals, but share
one construction family. Claim insufficiency is represented by one
equal-authority-conflict construction, not a separate missing-fact family.
Adversarial instructions are overt and repetitive, harmless-edit coverage is
absent, and no held-out typing cases are included.

## Protocol gates this corpus can support after review

If, and only if, humans approve or adjust the labels, this corpus can support:

- tuning examples at the protocol minimum of 30 per task;
- case-level relationship unsupported-acceptance point estimates over 150
  held-out negative rows;
- case-level claim harmful-acceptance point estimates over 150 held-out
  negative rows;
- contradiction recall on 50 proposed contradicted cases; and
- a limited relationship direction slice with 30 genuine reverse-edge cases,
  plus ordinary positive accuracy on the remaining paired controls.

It cannot yet support:

- any measured-quality claim before human adjudication;
- the protocol's independent-negative sample gate: each negative task has only
  two construction clusters, so neither the 150 distinct IDs nor the 150 case
  denominator justifies an independent-trial interval;
- held-out document-type or candidate-type quality (those tasks have tuning
  cases only here);
- model calibration, latency, cost, or provider reliability without an
  authorized run and preserved observations;
- candidate discovery recall, because candidates are supplied shortlists;
- statistical generalization beyond this synthetic construction; or
- live readiness. Provider pins, credentials, rates, token bounds, and budget
  authorization remain separate execution gates.

## Gold-free execution boundary

`fixtures.jsonl` and its manifest are adjudication/scoring artifacts and
contain gold. They are not request payloads. No request inventory is checked
in here. The accepted pilot inventory builder is currently specific to the
24-case approved tuning packet; it cannot prepare this 720-row corpus without
a separately reviewed extension. Any future authorized run must freeze a
separate gold-free inventory, and gold may be joined only at scoring time.

## Reproduction

All commands are offline:

```bash
cd evaluations/jev
go run ./heldout/generate -out heldout/fixtures.jsonl
go run ./cmd/jev-manifest \
  -fixtures heldout/fixtures.jsonl \
  -out heldout/fixtures.manifest.json \
  -generated-at 2026-09-22T00:00:00Z
go test -race -count=1 ./heldout ./loader
```

The generator is deterministic. The fixed manifest timestamp makes both files
byte-reproducible; the manifest validates record hashes, split counts, task
counts, challenge counts, review status, and structural split isolation.
