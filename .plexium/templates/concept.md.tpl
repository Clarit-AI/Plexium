---
title: {{.Title}}
ownership: managed
last-updated: {{.LastUpdated}}
review-status: unreviewed
tags: [{{range $i, $t := .Tags}}{{if $i}}, {{end}}{{$t}}{{end}}]
---

# {{.Title}}

{{.Description}}

{{if .Examples}}
## Examples

{{range .Examples}}
- {{.}}
{{end}}
{{end}}

{{if .Related}}
## Related Concepts

{{range .Related}}
- [[{{.}}]]
{{end}}
{{end}}
