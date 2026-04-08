# Memento Integration

Memento is one of the most distinctive parts of Plexium's story. Most systems inspired by the LLM Wiki idea can preserve final documentation. Very few have a path for preserving the reasoning, tradeoffs, and decision context that happened during the actual coding session.

Plexium can use Memento to fill that gap.

---

## What Memento Adds

Memento captures coding session provenance as git notes. Plexium can then treat that session history as raw source material instead of letting it disappear after the commit lands.

That gives Plexium access to things a code diff usually does not contain:

- design rationale
- tradeoffs that were considered and rejected
- temporary uncertainty resolved during implementation
- intent that never made it into comments or ADRs

This is what makes Memento more than an audit trail in the Plexium model. It becomes an input to documentation quality.

---

## How It Fits Into Plexium

When enabled, Memento sits beside the normal source and wiki workflow:

1. A coding session is captured as provenance.
2. Plexium copies or references that transcript material as raw input.
3. The assistive layer can extract useful decisions, rationale, and follow-up documentation from it.
4. The resulting knowledge can flow into module pages, ADR candidates, logs, or contradiction tracking.

That means Plexium is not limited to documenting what changed. It can also document why it changed.

---

## Why This Is Optional but Powerful

Plexium does not require Memento. The wiki, manifest, compile/lint pipeline, retrieval CLI, MCP setup, and marketplace flows all work without it.

But when you enable Memento, Plexium gains something rare: a durable source for session intent. That is especially valuable in teams using multiple agents, long-lived feature work, or autonomous maintenance passes where rationale would otherwise disappear.

---

## Secret Safety

Memento also raises the stakes on secret handling.

Never paste API keys, tokens, or other secrets into an AI chat window. In a Memento-enabled repo, that context can later be preserved as provenance and become part of the material Plexium may ingest or publish internally.

Prefer terminal-native setup instead:

```bash
export OPENROUTER_API_KEY="sk-or-v1-..."
plexium agent setup
```

If a secret was already pasted into chat:

- rewind the session if possible
- rotate the secret if needed
- do not commit that session to Memento

---

## What Plexium Can Extract

The useful output from transcript ingestion is not "the whole chat copied into the wiki." The valuable output is structured knowledge such as:

- ADR candidates
- rationale for module changes
- unresolved questions worth tracking
- contradictions between old assumptions and new implementation

That is the gap Memento helps close: preserving reasoning without turning the wiki into a transcript dump.

---

## Related Docs

- [How Plexium Works](how-it-works.md)
- [Automation and Hooks](automation-and-hooks.md)
- [Inspirations](inspirations.md)
- [User Guide](user-guide.md)
