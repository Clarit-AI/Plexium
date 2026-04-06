package generate

import (
	"testing"
)

func TestToSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Auth", "auth"},
		{"Auth Middleware", "auth-middleware"},
		{"auth-middleware", "auth-middleware"},
		{"my_api_client", "my-api-client"},
		{"APIv2", "apiv2"},
		{"auth middleware", "auth-middleware"},
		{"  spaces  ", "spaces"},
		{"special!@#$chars", "specialchars"},
		{"already-slugified", "already-slugified"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ToSlug(tt.input)
			if got != tt.want {
				t.Errorf("ToSlug(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDeduplicate_NoDuplicates(t *testing.T) {
	slugs := []string{"auth", "api", "database"}
	result := Deduplicate(slugs)

	if len(result.Ordered) != 3 {
		t.Errorf("expected 3 results, got %d", len(result.Ordered))
	}
	if result.ByIndex[0] != "auth" {
		t.Errorf("ByIndex[0] = %q, want %q", result.ByIndex[0], "auth")
	}
	if result.ByIndex[1] != "api" {
		t.Errorf("ByIndex[1] = %q, want %q", result.ByIndex[1], "api")
	}
	if result.ByIndex[2] != "database" {
		t.Errorf("ByIndex[2] = %q, want %q", result.ByIndex[2], "database")
	}
}

func TestDeduplicate_SimpleDuplicate(t *testing.T) {
	slugs := []string{"auth", "auth", "database"}
	result := Deduplicate(slugs)

	if len(result.Ordered) != 3 {
		t.Fatalf("expected 3 results, got %d: %v", len(result.Ordered), result.Ordered)
	}

	// First "auth" should stay as-is
	if result.ByIndex[0] != "auth" {
		t.Errorf("ByIndex[0] = %q, want %q", result.ByIndex[0], "auth")
	}
	// Second "auth" should get a suffix
	if result.ByIndex[1] == "auth" {
		t.Errorf("ByIndex[1] = %q, should be deduplicated", result.ByIndex[1])
	}
	if !startsWith(result.ByIndex[1], "auth-") {
		t.Errorf("ByIndex[1] = %q, should start with 'auth-'", result.ByIndex[1])
	}
	// "database" should stay as-is
	if result.ByIndex[2] != "database" {
		t.Errorf("ByIndex[2] = %q, want %q", result.ByIndex[2], "database")
	}

	// All output values must be unique
	seen := make(map[string]bool)
	for _, s := range result.Ordered {
		if seen[s] {
			t.Errorf("duplicate output slug: %q", s)
		}
		seen[s] = true
	}
}

func TestDeduplicate_TripleDuplicate(t *testing.T) {
	slugs := []string{"auth", "auth", "auth"}
	result := Deduplicate(slugs)

	if len(result.Ordered) != 3 {
		t.Fatalf("expected 3 results, got %d: %v", len(result.Ordered), result.Ordered)
	}

	seen := make(map[string]bool)
	for i, s := range result.Ordered {
		if seen[s] {
			t.Errorf("duplicate output slug at index %d: %q", i, s)
		}
		seen[s] = true
	}
}

func TestDeduplicate_AllUniqueOutputs(t *testing.T) {
	slugs := []string{"a", "a", "a", "a", "a"}
	result := Deduplicate(slugs)

	if len(result.Ordered) != 5 {
		t.Fatalf("expected 5 results, got %d", len(result.Ordered))
	}

	seen := make(map[string]bool)
	for _, s := range result.Ordered {
		if seen[s] {
			t.Errorf("duplicate output slug: %q in %v", s, result.Ordered)
		}
		seen[s] = true
	}
}

func TestPathToSlug(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"src/auth/middleware.go", "middleware"},
		{"src/auth.go", "auth"},
		{"src/auth/middleware/auth.go", "auth"},
		{"docs/guide.md", "guide"},
		{"adr/001-chose-postgres.md", "001-chose-postgres"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := PathToSlug(tt.path)
			if got != tt.want {
				t.Errorf("PathToSlug(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestResolveSlugConflict(t *testing.T) {
	existing := []string{"auth", "api", "database"}

	tests := []struct {
		base   string
		slug   string
		exists bool
	}{
		{"auth", "auth", true},
		{"session", "session", false},
	}

	for _, tt := range tests {
		t.Run(tt.base, func(t *testing.T) {
			isExisting := false
			for _, e := range existing {
				if e == tt.base {
					isExisting = true
					break
				}
			}
			if isExisting != tt.exists {
				t.Errorf("ResolveSlugConflict(%q, %v) exists check = %v, want %v",
					tt.slug, existing, isExisting, tt.exists)
			}
		})
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
