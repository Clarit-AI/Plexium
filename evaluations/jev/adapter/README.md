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
Local rejections also retain the frozen request digest and explicitly record
that no request/response occurred. Wire attempts distinguish request attempted
from response received. `Retry-After` and `Location` are safe allowlisted
evidence; the adapter never sleeps or follows the redirect itself.

Only HTTP 200 is admitted for semantic validation. Duplicate response keys or
non-canonical/extra Jev response properties fail schema admission before
billing or labels can become authoritative. Billing fields from any
schema-rejected response are retained as raw evidence but marked invalid.
Decimal validation accepts only JSON number tokens and uses exact decimal
arithmetic bounded by the signed 64-bit micro-unit accounting range; strings,
negative values, fractional token counts, and out-of-range amounts fail.

The strict Nano request shape includes `response_format.json_schema.strict`,
an exact one-field `label` enum schema, `max_tokens`, and provider routing
fields. The strict response path accepts one completed assistant choice and one
JSON object containing exactly one in-vocabulary string `label`. It rejects
refusals, truncation, extra or duplicate keys, trailing JSON, model-pin drift,
case variants of the exact lowercase `label` key, and missing/mismatched
provider identity. No confidence or probabilities are invented for Nano.

### Closed Nano response profile

The accepted response profile is the intersection needed by the frozen Nano
request and the documented OpenAI/OpenRouter chat-completion envelope. It is
closed at every level; keys not listed here are rejected:

- envelope: `id`, `object`, `created`, `model`, `provider`, `choices`, `usage`,
  `system_fingerprint`, `service_tier` (`id`, `model`, `provider`, `choices`,
  and `usage` are required); when present, `object` must be the exact string
  `chat.completion`;
- choice: `index`, `message`, `finish_reason`, `native_finish_reason`,
  `logprobs`;
- message: `role`, `content`, `refusal`, `reasoning`, `annotations`;
- annotation: exactly `type` and `url_citation`, where `type` is
  `url_citation`; the citation has exactly `end_index`, `start_index`, `title`,
  and `url`;
- usage: `prompt_tokens`, `prompt_tokens_details`, `completion_tokens`,
  `completion_tokens_details`, `total_tokens`, `cost`, `cost_details`,
  `is_byok`;
- prompt details: `cached_tokens`, `cache_write_tokens`, `audio_tokens`,
  `video_tokens`; completion details: `reasoning_tokens`, `image_tokens`,
  `audio_tokens`, `accepted_prediction_tokens`, `rejected_prediction_tokens`;
  cost details: `upstream_inference_cost`,
  `upstream_inference_prompt_cost`, and
  `upstream_inference_completions_cost`.

The OpenAI Chat Completions response reference documents `annotations`, its
closed `url_citation` shape, and the accepted/rejected prediction counters:
<https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create>.
The compatible OpenRouter response and metadata header are documented at
<https://openrouter.ai/docs/api/api-reference/chat/create-a-chat-completion>.
`openrouter_metadata` is deliberately excluded: the frozen request did not
enable it. OpenRouter exposes it through the opt-in `X-OpenRouter-Metadata`
header (and the legacy `X-OpenRouter-Experimental-Metadata` header), so any
future enablement requires an explicit reviewed request-configuration and
schema change; it need not require a request-body change. Tool-call, audio,
and other response variants are likewise outside this frozen text-only
structured-output profile rather than silently wildcarded.

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
