package convert

import (
	"os"
	"path/filepath"
	"strings"
)

func loadRepoIgnorePatterns(repoRoot string) []string {
	var patterns []string
	for _, name := range []string{".gitignore", ".plexiumignore"} {
		patterns = append(patterns, readIgnorePatterns(filepath.Join(repoRoot, name))...)
	}
	return patterns
}

func readIgnorePatterns(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var patterns []string
	for _, line := range strings.Split(string(data), "\n") {
		pattern := strings.TrimSpace(line)
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		if strings.HasPrefix(pattern, "!") {
			return nil
		}
		patterns = append(patterns, expandIgnorePattern(pattern)...)
	}
	return patterns
}

func expandIgnorePattern(pattern string) []string {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	pattern = strings.TrimPrefix(pattern, "/")
	if pattern == "" {
		return nil
	}

	dirPattern := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	if pattern == "" {
		return nil
	}

	var patterns []string
	add := func(value string) {
		if value == "" {
			return
		}
		for _, existing := range patterns {
			if existing == value {
				return
			}
		}
		patterns = append(patterns, value)
	}

	add(pattern)
	if !strings.Contains(pattern, "/") {
		add("**/" + pattern)
	}
	if dirPattern || !hasGlobMeta(pattern) {
		add(pattern + "/**")
		if !strings.Contains(pattern, "/") {
			add("**/" + pattern + "/**")
		}
	}

	return patterns
}

func hasGlobMeta(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}
