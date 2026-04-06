package generate

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// ToSlug converts a title to a filesystem-safe slug
func ToSlug(title string) string {
	var result strings.Builder
	var lastChar rune
	for i, r := range strings.ToLower(title) {
		if i == 0 && !unicode.IsLetter(r) {
			continue
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			result.WriteRune(r)
			lastChar = r
		case r == ' ' || r == '_' || r == '-':
			if i > 0 && result.Len() > 0 && lastChar != '-' {
				result.WriteByte('-')
				lastChar = '-'
			}
		}
	}

	slug := result.String()
	// Remove trailing dashes
	slug = strings.TrimRight(slug, "-")
	return slug
}

// DeduplicateResult holds the result of deduplicating a list of slugs.
// The Ordered field preserves the deduplicated slugs in input order.
// The ByIndex field maps input index to its deduplicated slug.
type DeduplicateResult struct {
	Ordered  []string          // Deduplicated slugs in input order
	ByIndex  map[int]string    // Input index → deduplicated slug
	BySlug   map[string]string // Original slug → deduplicated slug (last wins for dupes)
}

// Deduplicate resolves slug collisions by appending suffixes.
// Returns a DeduplicateResult that preserves all input-to-output mappings.
func Deduplicate(slugs []string) *DeduplicateResult {
	result := &DeduplicateResult{
		Ordered: make([]string, 0, len(slugs)),
		ByIndex: make(map[int]string, len(slugs)),
		BySlug:  make(map[string]string, len(slugs)),
	}

	used := make(map[string]bool)

	for i, slug := range slugs {
		if !used[slug] {
			// First occurrence — use as-is
			used[slug] = true
			result.Ordered = append(result.Ordered, slug)
			result.ByIndex[i] = slug
			result.BySlug[slug] = slug
		} else {
			// Duplicate — find a unique suffixed version
			candidate := slug
			for suffix := 2; ; suffix++ {
				candidate = slug + "-" + formatAlpha(suffix)
				if !used[candidate] {
					break
				}
			}
			used[candidate] = true
			result.Ordered = append(result.Ordered, candidate)
			result.ByIndex[i] = candidate
			result.BySlug[slug] = candidate
		}
	}

	return result
}

// formatAlpha converts 2->"b", 3->"c", 4->"d", etc.
func formatAlpha(n int) string {
	if n <= 1 {
		return ""
	}
	n--
	result := ""
	for n >= 0 {
		result = string(rune('a'+n%26)) + result
		n = n/26 - 1
		if n < 0 {
			break
		}
	}
	return result
}

// DeduplicateWithPaths handles slug generation from file paths
func DeduplicateWithPaths(paths []string) *DeduplicateResult {
	slugs := make([]string, len(paths))
	for i, p := range paths {
		slugs[i] = PathToSlug(p)
	}
	return Deduplicate(slugs)
}

// PathToSlug converts a file path to a slug
func PathToSlug(filePath string) string {
	// Get base name without extension
	filename := filepath.Base(filePath)
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)

	// Handle special directories
	if strings.Contains(filePath, "src/") {
		parts := strings.Split(filePath, "/")
		if len(parts) >= 2 {
			name = parts[len(parts)-1]
			if ext := filepath.Ext(name); ext != "" {
				name = strings.TrimSuffix(name, ext)
			}
		}
	}

	// Check if this is an ADR (numeric prefix like 001-)
	adrPrefix := regexp.MustCompile(`^\d+-`)
	if adrPrefix.MatchString(name) {
		// Preserve ADR slug format (leading zeros)
		return name
	}

	return ToSlug(name)
}

// extractQualifier extracts a distinguishing qualifier from a path
func extractQualifier(filePath string) string {
	filePath = strings.TrimSuffix(filePath, filepath.Ext(filePath))

	parts := strings.Split(filePath, "/")

	if len(parts) >= 3 {
		last := parts[len(parts)-1]
		secondLast := parts[len(parts)-2]
		return last + "-" + secondLast
	}

	if len(parts) == 2 {
		return parts[1]
	}

	return ""
}

// ResolveSlugConflict generates a unique slug for a conflicting name
func ResolveSlugConflict(baseSlug string, existingSlugs []string) string {
	slugSet := make(map[string]bool)
	for _, s := range existingSlugs {
		slugSet[s] = true
	}

	if !slugSet[baseSlug] {
		return baseSlug
	}

	for i := 2; i <= 26; i++ {
		newSlug := baseSlug + "-" + formatAlpha(i)
		if !slugSet[newSlug] {
			return newSlug
		}
	}

	return baseSlug + "-1"
}

// SectionSlug returns the wiki section path for a slug
func SectionSlug(section, slug string) string {
	switch section {
	case "Root", "Home":
		return "Home.md"
	case "Modules":
		return path.Join("modules", slug+".md")
	case "Decisions":
		return path.Join("decisions", slug+".md")
	case "Concepts":
		return path.Join("concepts", slug+".md")
	case "Patterns":
		return path.Join("patterns", slug+".md")
	case "Architecture":
		return path.Join("architecture", slug+".md")
	case "Guides":
		return path.Join("guides", slug+".md")
	default:
		return path.Join(strings.ToLower(section), slug+".md")
	}
}
