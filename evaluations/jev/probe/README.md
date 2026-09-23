# Pin-probe execution

The pin probe is a four-request immutable plan. `--dry-run` prints the full
plan, including the request body hashes and byte counts, without reading the
credential or dialing. `--execute` uses the accepted one-shot adapters and
requires a new private state directory.

## Closed request-subset contract

`--request-ordinals` exists only to complete an explicitly approved subset of
the immutable plan after an earlier run halted. Its value is a comma-separated
set of positive integer ordinals from the approved plan, for example:

```text
--request-ordinals=2,3,4
```

The grammar admits digits and commas only. Empty values, empty elements,
duplicates, ranges, wildcards, words, zero, negative values, and ordinals not
present in the approved plan are rejected before credential lookup or any
dial. Every CLI flag is single-occurrence, and the command accepts no
positional arguments or arguments after `--`; these forms are also rejected
before credential lookup. String flag values beginning with `-` are rejected
so a missing value cannot consume the next flag. A real path whose basename
begins with a hyphen must use a non-hyphen-leading form such as
`--state-dir ./-weirdname`. Input order is normalized to immutable plan order.
Omitting the flag retains the full four-request behavior.

Both the plan and the durable report record `selectedOrdinals` and
`selectedRequests`. Each selected request binding includes its ordinal, plan
ID, arm, fixture/kind, endpoint, requested alias, expected response pin and
provider, request SHA-256 and byte count, and reservation. Selection changes
only which approved entries execute; it never rebuilds or modifies their
request bytes.

A subset is not self-authorizing. Operators must use it only when the exact
ordinals have been approved for plan completion. Existing state still refuses
before dial, and all reservation, one-shot, anomaly-halt, settlement,
credential-screening, gate-status, accounting-basis, and execution-readiness
rules remain identical to the full-plan path.

## Offline derived assessment

`--derive-assessment` recomputes gate coverage from immutable report rows
without reading credentials or dialing. It requires comma-separated source
reports and a new output path:

```text
--derive-assessment --source-reports=<run-1-report>,<run-2-report> \
  --assessment-out=<new-derived-report> --inventory=<accepted-inventory>
```

The output is created exclusively and cannot overwrite a source. Source
SHA-256 values are captured and rechecked after the write. Gate promotion is
based on selected request bindings plus identity-matched accepted attempts,
not attempt cardinality. The derived accounting keeps provider-reported raw
cost, admitted cost, untrusted reported cost, and conservative ledger exposure
separate; provider-reported raw cost is not described as verified spend.

Derivation validates each source independently before merging it. Selection
ordinals and bindings must be unique and consistent, and every attempt must
belong to that source's declared selection. Reports predating selection fields
retain the legacy full-plan fallback only when both fields are absent. Within
one source, any repeated attempt ordinal is rejected before coverage or cost
aggregation, regardless of outcome. Across sources, the same ordinal is a
legitimate retry only when it has both a new reservation reference and new,
comparable response evidence (raw-response or persisted-evidence hash).
Mutable provenance such as start time or provider request ID never establishes
a new attempt. Repeated source content or reused/conflicting stable evidence is
rejected. JSON property presence is distinct from slice length: the legacy
full-plan fallback applies only when both selection properties are absent;
present empty or null selections are rejected.
