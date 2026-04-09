---
title: Plexium Prompt Pack
ownership: managed
---

# Plexium Prompt Pack

These files are the editable prompt contracts for Plexium's assistive workflows.

- `assistive/` contains role and task prompts.
- `profiles/` contains capability-profile overlays that tune those prompts for local vs frontier models.

Plexium writes these files into each repository under `.plexium/prompts/` during setup. You can edit the repo-local copies to refine behavior without recompiling Plexium.
