package beads

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// BeadsLinker provides bidirectional linking between bd tasks and wiki pages.
// Task IDs are stored in wiki page frontmatter as `beads-ids: [...]`.
type BeadsLinker struct {
	WikiRoot string
	BdPath   string // path to bd executable, defaults to "bd"
}

// LinkResult holds the outcome of a link operation.
type LinkResult struct {
	TaskID   string
	WikiPath string
	Action   string // "added", "removed", "already-linked", "not-linked", "error"
}

// TaskPageMapping represents a bidirectional mapping between a task and its wiki pages.
type TaskPageMapping struct {
	TaskID    string   `json:"taskId"`
	WikiPaths []string `json:"wikiPaths"`
}

// PageTaskMapping represents the tasks linked to a wiki page.
type PageTaskMapping struct {
	WikiPath string   `json:"wikiPath"`
	TaskIDs  []string `json:"taskIds"`
}

// NewLinker creates a new BeadsLinker with defaults.
func NewLinker(wikiRoot string) *BeadsLinker {
	return &BeadsLinker{
		WikiRoot: wikiRoot,
		BdPath:   "bd",
	}
}

// GetTaskPages returns wiki pages linked to a given task ID by scanning frontmatter.
func (l *BeadsLinker) GetTaskPages(taskID string) (*TaskPageMapping, error) {
	mapping := &TaskPageMapping{
		TaskID:    taskID,
		WikiPaths: []string{},
	}

	err := l.walkWikiPages(func(path string) error {
		fm, _, err := readFrontmatter(path)
		if err != nil {
			return nil // skip files with bad frontmatter
		}
		ids := getBeadsIDs(fm)
		for _, id := range ids {
			if id == taskID {
				rel, err := filepath.Rel(l.WikiRoot, path)
				if err != nil {
					rel = path
				}
				mapping.WikiPaths = append(mapping.WikiPaths, rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning wiki pages: %w", err)
	}

	return mapping, nil
}

// GetPageTasks reads a wiki page's frontmatter and returns its beads-ids.
func (l *BeadsLinker) GetPageTasks(wikiPath string) (*PageTaskMapping, error) {
	fullPath := l.resolvePath(wikiPath)

	fm, _, err := readFrontmatter(fullPath)
	if err != nil {
		return nil, fmt.Errorf("reading frontmatter from %s: %w", wikiPath, err)
	}

	return &PageTaskMapping{
		WikiPath: wikiPath,
		TaskIDs:  getBeadsIDs(fm),
	}, nil
}

// LinkTaskToPage adds a task ID to a wiki page's beads-ids frontmatter field.
// Returns error if page doesn't exist. Idempotent — won't add duplicates.
func (l *BeadsLinker) LinkTaskToPage(taskID, wikiPath string) (*LinkResult, error) {
	fullPath := l.resolvePath(wikiPath)

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("wiki page does not exist: %s", wikiPath)
	}

	fm, body, err := readFrontmatter(fullPath)
	if err != nil {
		return nil, fmt.Errorf("reading frontmatter from %s: %w", wikiPath, err)
	}

	ids := getBeadsIDs(fm)

	// Check for duplicates
	for _, id := range ids {
		if id == taskID {
			return &LinkResult{
				TaskID:   taskID,
				WikiPath: wikiPath,
				Action:   "already-linked",
			}, nil
		}
	}

	// Add the task ID
	ids = append(ids, taskID)
	fm["beads-ids"] = ids

	if err := writeFrontmatter(fullPath, fm, body); err != nil {
		return nil, fmt.Errorf("writing frontmatter to %s: %w", wikiPath, err)
	}

	return &LinkResult{
		TaskID:   taskID,
		WikiPath: wikiPath,
		Action:   "added",
	}, nil
}

// UnlinkTaskFromPage removes a task ID from a wiki page's beads-ids.
func (l *BeadsLinker) UnlinkTaskFromPage(taskID, wikiPath string) (*LinkResult, error) {
	fullPath := l.resolvePath(wikiPath)

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("wiki page does not exist: %s", wikiPath)
	}

	fm, body, err := readFrontmatter(fullPath)
	if err != nil {
		return nil, fmt.Errorf("reading frontmatter from %s: %w", wikiPath, err)
	}

	ids := getBeadsIDs(fm)

	// Find and remove the task ID
	found := false
	newIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == taskID {
			found = true
			continue
		}
		newIDs = append(newIDs, id)
	}

	if !found {
		return &LinkResult{
			TaskID:   taskID,
			WikiPath: wikiPath,
			Action:   "not-linked",
		}, nil
	}

	if len(newIDs) > 0 {
		fm["beads-ids"] = newIDs
	} else {
		delete(fm, "beads-ids")
	}

	if err := writeFrontmatter(fullPath, fm, body); err != nil {
		return nil, fmt.Errorf("writing frontmatter to %s: %w", wikiPath, err)
	}

	return &LinkResult{
		TaskID:   taskID,
		WikiPath: wikiPath,
		Action:   "removed",
	}, nil
}

// ScanAllLinks scans all wiki pages and builds the complete bidirectional map.
func (l *BeadsLinker) ScanAllLinks() ([]TaskPageMapping, error) {
	taskMap := make(map[string][]string) // taskID -> []wikiPath

	err := l.walkWikiPages(func(path string) error {
		fm, _, err := readFrontmatter(path)
		if err != nil {
			return nil // skip files with bad frontmatter
		}
		ids := getBeadsIDs(fm)
		if len(ids) == 0 {
			return nil
		}

		rel, err := filepath.Rel(l.WikiRoot, path)
		if err != nil {
			rel = path
		}

		for _, id := range ids {
			taskMap[id] = append(taskMap[id], rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning wiki pages: %w", err)
	}

	result := make([]TaskPageMapping, 0, len(taskMap))
	for taskID, paths := range taskMap {
		result = append(result, TaskPageMapping{
			TaskID:    taskID,
			WikiPaths: paths,
		})
	}

	return result, nil
}

// resolvePath resolves a wiki path to an absolute path.
// If the path is already absolute, returns it as-is.
// Otherwise, joins it with WikiRoot.
func (l *BeadsLinker) resolvePath(wikiPath string) string {
	if filepath.IsAbs(wikiPath) {
		return wikiPath
	}
	return filepath.Join(l.WikiRoot, wikiPath)
}

// walkWikiPages walks .wiki/ recursively, calling fn for each .md file.
// Skips files starting with '_' (like _schema.md, _index.md).
func (l *BeadsLinker) walkWikiPages(fn func(path string) error) error {
	return filepath.Walk(l.WikiRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		if strings.HasPrefix(info.Name(), "_") {
			return nil
		}
		return fn(path)
	})
}

// readFrontmatter reads YAML frontmatter from a markdown file.
// Returns the frontmatter as a map, the body content (after frontmatter), and any error.
func readFrontmatter(path string) (map[string]interface{}, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("reading file: %w", err)
	}

	content := string(data)

	// Frontmatter must start with "---\n"
	if !strings.HasPrefix(content, "---\n") {
		return nil, content, fmt.Errorf("no frontmatter found")
	}

	// Find the closing "---"
	rest := content[4:] // skip opening "---\n"
	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		return nil, content, fmt.Errorf("unclosed frontmatter")
	}

	fmRaw := rest[:idx]
	// Body starts after the closing "---\n"
	body := rest[idx+4:]

	fm := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(fmRaw), &fm); err != nil {
		return nil, content, fmt.Errorf("parsing frontmatter YAML: %w", err)
	}

	return fm, body, nil
}

// writeFrontmatter writes updated frontmatter + body back to a file.
func writeFrontmatter(path string, fm map[string]interface{}, body string) error {
	var buf bytes.Buffer

	buf.WriteString("---\n")

	// Marshal frontmatter with stable key ordering via yaml.v3 encoder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return fmt.Errorf("encoding frontmatter: %w", err)
	}
	enc.Close()

	// yaml.v3 Encoder writes a trailing "...\n" document-end marker; strip it
	// and replace with the frontmatter closing delimiter.
	raw := buf.String()
	raw = strings.TrimSuffix(raw, "...\n")
	raw = strings.TrimSuffix(raw, "\n")

	var out bytes.Buffer
	out.WriteString(raw)
	out.WriteString("\n---")
	out.WriteString(body)

	return os.WriteFile(path, out.Bytes(), 0644)
}

// getBeadsIDs extracts beads-ids from frontmatter map as []string.
// Handles various YAML representations: string slices, interface slices, single strings.
func getBeadsIDs(fm map[string]interface{}) []string {
	raw, ok := fm["beads-ids"]
	if !ok {
		return nil
	}

	switch v := raw.(type) {
	case []interface{}:
		ids := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				ids = append(ids, s)
			}
		}
		return ids
	case []string:
		return v
	case string:
		// Single value: treat as a one-element list
		return []string{v}
	}

	return nil
}

// readFrontmatterKeys returns frontmatter keys in file order.
// This is used internally for ordering when writing back.
func readFrontmatterKeys(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var keys []string
	inFM := false

	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			if !inFM {
				inFM = true
				continue
			}
			break // end of frontmatter
		}
		if inFM && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && strings.Contains(line, ":") {
			key := strings.SplitN(line, ":", 2)[0]
			keys = append(keys, strings.TrimSpace(key))
		}
	}
	return keys, scanner.Err()
}
