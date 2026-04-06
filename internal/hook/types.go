package hook

// HookResult is the result of a pre-commit hook check.
type HookResult struct {
	Allowed     bool     `json:"allowed"`
	Strictness  string   `json:"strictness"` // "strict", "moderate", "advisory"
	Reason      string   `json:"reason,omitempty"`
	FilesChanged []string `json:"filesChanged"`
	WikiUpdated  bool    `json:"wikiUpdated"`
	Skipped      bool    `json:"skipped,omitempty"`
	SkipReason   string  `json:"skipReason,omitempty"`
}

// WikiDebtEntry represents a WIKI-DEBT log entry in _log.md.
type WikiDebtEntry struct {
	Date        string   `json:"date"`
	CommitSHA   string   `json:"commitSha"`
	Files       []string `json:"files"`
	BypassedBy  string   `json:"bypassedBy"`
	Status      string   `json:"status"` // "pending wiki update"
}
