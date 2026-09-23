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
before credential lookup. Input order is normalized to immutable plan order.
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
