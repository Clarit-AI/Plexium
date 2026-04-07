package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v2"
)

// ComputeHash returns the SHA256 hash of a file's content.
// Hash is of file content only — not path or metadata.
func ComputeHash(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("reading file for hash %s: %w", filePath, err)
	}
	return hashBytes(data), nil
}

// ComputeHashString returns the SHA256 hash of a string.
func ComputeHashString(content string) string {
	return hashBytes([]byte(content))
}

// ComputeDirHash returns a combined hash of all files matching globs in a directory.
// Individual file hashes are sorted by path for determinism.
func ComputeDirHash(dirPath string, globs []string) (string, error) {
	type fileHash struct {
		path string
		hash string
	}

	var hashes []fileHash

	for _, pattern := range globs {
		// Use doublestar.Glob which properly handles ** patterns and returns
		// results sorted by path on all platforms for deterministic hashing.
		matches, err := doublestar.Glob(filepath.Join(dirPath, pattern))
		if err != nil {
			return "", fmt.Errorf("globbing %s in %s: %w", pattern, dirPath, err)
		}

		for _, match := range matches {
			// Skip directories
			info, err := os.Stat(match)
			if err != nil {
				continue
			}
			if info.IsDir() {
				continue
			}

			h, err := ComputeHash(match)
			if err != nil {
				continue
			}

			relPath, err := filepath.Rel(dirPath, match)
			if err != nil {
				relPath = match
			}
			hashes = append(hashes, fileHash{path: filepath.ToSlash(relPath), hash: h})
		}
	}

	// Sort by path for deterministic output
	sort.Slice(hashes, func(i, j int) bool {
		return hashes[i].path < hashes[j].path
	})

	// Combine all hashes
	var combined strings.Builder
	for _, fh := range hashes {
		combined.WriteString(fh.path)
		combined.WriteString(":")
		combined.WriteString(fh.hash)
		combined.WriteString("\n")
	}

	return hashBytes([]byte(combined.String())), nil
}

// HashAllSources computes hashes for all source files in a PageEntry.
// Returns a map of source path → hash.
func HashAllSources(entry PageEntry) (map[string]string, error) {
	result := make(map[string]string, len(entry.SourceFiles))

	for _, sf := range entry.SourceFiles {
		h, err := ComputeHash(sf.Path)
		if err != nil {
			return nil, fmt.Errorf("hashing source file %s: %w", sf.Path, err)
		}
		result[sf.Path] = h
	}

	return result, nil
}

// hashBytes computes SHA256 of raw bytes and returns hex-encoded string.
func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
