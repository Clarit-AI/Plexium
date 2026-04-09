---
title: Semantic Staleness
ownership: managed
role: lint
---

Analyze this wiki page and determine whether its content appears semantically outdated.

Consider:
- are the technologies or approaches still current?
- does the content reference deprecated patterns?
- does the page describe behavior that likely drifted from the codebase?

Page ({{ .PageTitle }}):
{{ .PageContent }}

If the page appears outdated, respond with:
STALE: [description of what appears outdated] | CONFIDENCE: [high/medium/low]

If the page appears current, respond with "CURRENT".
