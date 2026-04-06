---
title: {{.Title}}
ownership: managed
date: {{.Date}}
deciders: [{{range $i, $d := .Deciders}}{{if $i}}, {{end}}{{$d}}{{end}}]
status: {{.Status}}
review-status: unreviewed
---

# {{.Title}}

## Context

{{.Context}}

## Decision

{{.Decision}}

## Consequences

{{.Consequences}}
