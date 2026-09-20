# Offline one-shot adapter contract

`SubmitDecisionsOnce` and `CompleteOnce` are Stage 1 transport primitives for
the KHA-579 tuning pilot. They accept already-frozen request bytes and perform
exactly one POST. They do not retry, sleep, follow redirects, repair output, or
alter the request body. Existing `SubmitDecisions` and `Complete` APIs retain
their compatibility behavior, including their configured retry policy.

Every one-shot call returns an `AttemptObservation`, including on HTTP,
transport, body-read, pin, provider, and schema failures. The observation
contains bounded raw response bytes and a SHA-256 digest, safe allowlisted
headers, request/route identity, timestamps, and presence-aware decimal
billing/usage fields. Missing, `null`, numeric zero, positive decimal, and
invalid fields remain distinct. Raw bytes are never copied into one-shot error
messages; a future runner must persist them privately with mode `0600`.

The strict Nano request shape includes `response_format.json_schema.strict`,
an exact one-field `label` enum schema, `max_tokens`, and provider routing
fields. The strict response path accepts one completed assistant choice and one
JSON object containing exactly one in-vocabulary string `label`. It rejects
refusals, truncation, extra or duplicate keys, trailing JSON, model-pin drift,
and missing/mismatched provider identity. No confidence or probabilities are
invented for Nano.

The request alias and accepted response pin are separate configuration fields.
For compatibility, `ResponseModel` defaults to `Model`; a pilot must supply an
explicit verified alias-to-pin mapping. `ResponseProvider` is also explicit.
Request routing is only a preference and never substitutes for response-linked
provider evidence.

## Live execution gates

This package does not authorize or perform a provider run by itself. The
following contracts remain intentionally unfrozen and must be independently
verified before any live runner can pass its execution gates:

- exact request-alias to returned-model-pin mappings for both arms;
- response-linked provider identity fields and Decisions routing support;
- Nano output-limit parameter name, semantics, and all billable categories;
- a finite billed-output bound for Jev suitable for the existing ledger;
- current rates, currency/fee semantics, token accounting, and tokenizer-bound
  input proofs.

All Stage 1 tests use local `httptest` servers or deterministic transports. No
credentials, external requests, runner, journal, or ledger behavior is added.
