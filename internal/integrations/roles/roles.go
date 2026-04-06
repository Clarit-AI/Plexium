package roles

// Role represents an agent role in the Plexium workflow.
// Phase 10 orchestration assigns these to actual agents; this package
// defines the pattern and provides role context construction.
type Role string

const (
	RoleCoder      Role = "coder"
	RoleRetriever  Role = "retriever"
	RoleDocumenter Role = "documenter"
	RoleIngestor   Role = "ingestor"
)

// RoleContext provides the working context for an agent operating in a specific role.
type RoleContext struct {
	Role            Role     `json:"role"`
	TaskDescription string   `json:"taskDescription"`
	WikiPath        string   `json:"wikiPath"`
	SourceFiles     []string `json:"sourceFiles"`
}

// RoleCapability describes what a role is allowed and expected to do.
type RoleCapability struct {
	Role        Role     `json:"role"`
	Description string   `json:"description"`
	CanRead     []string `json:"canRead"`
	CanWrite    []string `json:"canWrite"`
}

// Registry holds the known role capabilities.
var Registry = map[Role]RoleCapability{
	RoleCoder: {
		Role:        RoleCoder,
		Description: "Writes and modifies source code. Reads wiki for context but does not modify it.",
		CanRead:     []string{"src/**", ".wiki/**"},
		CanWrite:    []string{"src/**", "internal/**", "cmd/**"},
	},
	RoleRetriever: {
		Role:        RoleRetriever,
		Description: "Searches wiki and codebase via PageIndex. Compiles context for other roles.",
		CanRead:     []string{".wiki/**", "src/**", ".plexium/**"},
		CanWrite:    []string{},
	},
	RoleDocumenter: {
		Role:        RoleDocumenter,
		Description: "Updates wiki pages, maintains cross-references, runs lint.",
		CanRead:     []string{".wiki/**", "src/**", ".plexium/**"},
		CanWrite:    []string{".wiki/**", ".plexium/manifest.json"},
	},
	RoleIngestor: {
		Role:        RoleIngestor,
		Description: "Processes raw sources (transcripts, tickets, notes) into wiki pages.",
		CanRead:     []string{".wiki/raw/**", ".wiki/**"},
		CanWrite:    []string{".wiki/**", ".plexium/manifest.json"},
	},
}

// NewContext creates a RoleContext for the given role and task.
func NewContext(role Role, task, wikiPath string, sourceFiles []string) *RoleContext {
	return &RoleContext{
		Role:            role,
		TaskDescription: task,
		WikiPath:        wikiPath,
		SourceFiles:     sourceFiles,
	}
}

// Capabilities returns the capability definition for a role, or nil if unknown.
func Capabilities(role Role) *RoleCapability {
	cap, ok := Registry[role]
	if !ok {
		return nil
	}
	return &cap
}

// AllRoles returns all defined roles.
func AllRoles() []Role {
	return []Role{RoleCoder, RoleRetriever, RoleDocumenter, RoleIngestor}
}
