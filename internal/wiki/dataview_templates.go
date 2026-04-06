package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DataviewQueriesTemplate holds the dataview query templates.
const DataviewQueriesTemplate = `---
title: Dataview Queries
ownership: managed
last-updated: {{DATE}}
tags:
  - plexium/internal
---

# Dataview Query Templates

This page contains pre-built Dataview queries for monitoring wiki health.
These queries are automatically available in Obsidian when the Dataview plugin is enabled.

## Recently Updated Modules

~~~dataview
TABLE last-updated, updated-by, confidence
FROM "modules"
SORT last-updated DESC
LIMIT 10
~~~

## Unreviewed Pages

~~~dataview
LIST
FROM ""
WHERE review-status = "unreviewed"
SORT last-updated ASC
~~~

## Wiki Debt

~~~dataview
LIST
FROM "_log"
WHERE contains(file.content, "WIKI-DEBT")
~~~

## Stale Pages (>30 days)

~~~dataview
LIST
FROM ""
WHERE review-status = "stale"
SORT last-updated ASC
~~~

## Human-Authored Pages

~~~dataview
LIST
FROM ""
WHERE ownership = "human-authored"
SORT file.ctime DESC
~~~

## Managed Pages

~~~dataview
LIST
FROM ""
WHERE ownership = "managed"
SORT file.mtime DESC
~~~

## All Pages by Section

~~~dataview
TABLE section, title, ownership
FROM ""
WHERE section
SORT section ASC, title ASC
~~~

## Pages Missing Source Files

~~~dataview
LIST title, sourceFiles
FROM ""
WHERE length(sourceFiles) = 0 AND ownership = "managed"
~~~

## Orphan Pages (no inbound links)

~~~dataview
LIST title, inboundLinks
FROM ""
WHERE length(inboundLinks) = 0 AND file.name != "Home"
SORT title ASC
~~~

## Contradictions

~~~dataview
LIST
FROM "contradictions.md"
~~~
`

// EnsureTemplates creates the templates directory and dataview queries.
func EnsureTemplates(repoRoot string, dryRun bool) error {
	templatesDir := filepath.Join(repoRoot, ".wiki", "templates")

	if dryRun {
		fmt.Printf("[dry-run] Would create templates in %s\n", templatesDir)
		return nil
	}

	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		return fmt.Errorf("creating templates directory: %w", err)
	}

	// Write dataview-queries.md
	queriesPath := filepath.Join(templatesDir, "dataview-queries.md")
	content := replaceDatePlaceholders(DataviewQueriesTemplate)
	if err := os.WriteFile(queriesPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing dataview-queries.md: %w", err)
	}

	return nil
}

func replaceDatePlaceholders(template string) string {
	dateStr := time.Now().Format("2006-01-02")
	return strings.ReplaceAll(template, "{{DATE}}", dateStr)
}
