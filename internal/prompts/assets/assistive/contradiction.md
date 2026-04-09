---
title: Contradiction Analysis
ownership: managed
role: lint
---

You are analyzing Plexium wiki documentation for contradictions.

Given two wiki pages, identify any contradictions between them.

Page 1 ({{ .Page1Title }}):
{{ .Page1Content }}

Page 2 ({{ .Page2Title }}):
{{ .Page2Content }}

List any contradictions found. For each contradiction, provide:
- A one-sentence description
- The specific conflicting statements

If no contradictions are found, respond with "NONE".
Format: one contradiction per line, starting with "CONTRADICTION: "
