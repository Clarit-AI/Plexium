package hook

// HookResult is the result of a pre-commit hook check.
type HookResult struct {
	Allowed      bool     `json:"allowed"`
	Strictness   string   `json:"strictness"` // "strict", "moderate", "advisory"
	Reason       string   `json:"reason,omitempty"`
	FilesChanged []string `json:"filesChanged"`
	WikiUpdated  bool     `json:"wikiUpdated"`
	// WikiRelevant is true when a staged wiki change is mapped (via the
	// manifest's SourceFiles) to a staged source change, OR when an
	// explicit debt mark covers the staged source. KHA-287 / F7: a bare
	// wiki edit to an unrelated page must not satisfy a source change.
	WikiRelevant bool     `json:"wikiRelevant"`
	WikiFiles    []string `json:"wikiFiles,omitempty"`
	Skipped      bool     `json:"skipped,omitempty"`
	SkipReason   string   `json:"skipReason,omitempty"`
}

// WikiDebtEntry represents a WIKI-DEBT log entry in _log.md.
type WikiDebtEntry struct {
	Date       string   `json:"date"`
	CommitSHA  string   `json:"commitSha"`
	Files      []string `json:"files"`
	BypassedBy string   `json:"bypassedBy"`
	Status     string   `json:"status"` // "pending wiki update"
}
