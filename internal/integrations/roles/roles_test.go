package roles

import "testing"

func TestAllRoles(t *testing.T) {
	roles := AllRoles()
	if len(roles) != 4 {
		t.Fatalf("expected 4 roles, got %d", len(roles))
	}
	expected := map[Role]bool{RoleCoder: true, RoleRetriever: true, RoleDocumenter: true, RoleIngestor: true}
	for _, r := range roles {
		if !expected[r] {
			t.Errorf("unexpected role: %s", r)
		}
	}
}

func TestCapabilities(t *testing.T) {
	for _, role := range AllRoles() {
		cap := Capabilities(role)
		if cap == nil {
			t.Errorf("no capabilities for role %s", role)
			continue
		}
		if cap.Description == "" {
			t.Errorf("empty description for role %s", role)
		}
	}
}

func TestCapabilitiesUnknown(t *testing.T) {
	cap := Capabilities(Role("unknown"))
	if cap != nil {
		t.Error("expected nil for unknown role")
	}
}

func TestNewContext(t *testing.T) {
	ctx := NewContext(RoleCoder, "implement feature X", ".wiki/", []string{"main.go"})
	if ctx.Role != RoleCoder {
		t.Errorf("expected coder role, got %s", ctx.Role)
	}
	if ctx.TaskDescription != "implement feature X" {
		t.Errorf("wrong task description: %s", ctx.TaskDescription)
	}
	if len(ctx.SourceFiles) != 1 || ctx.SourceFiles[0] != "main.go" {
		t.Errorf("wrong source files: %v", ctx.SourceFiles)
	}
}

func TestRetrieverCannotWrite(t *testing.T) {
	cap := Capabilities(RoleRetriever)
	if len(cap.CanWrite) != 0 {
		t.Error("retriever should not have write permissions")
	}
}
