package migrate

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Migrator applies schema migrations to the wiki.
type Migrator struct {
	repoRoot    string
	wikiRoot    string
	plexiumDir  string
}

// Migration represents a single migration script.
type Migration struct {
	Number  int
	Name    string
	Path    string
	Version int
}

// MigrationResult holds the result of a migration run.
type MigrationResult struct {
	CurrentVersion int
	TargetVersion  int
	Applied        []Migration
	DryRun         bool
	Errors         []string
}

// NewMigrator creates a new migrator.
func NewMigrator(repoRoot, wikiRoot string) *Migrator {
	if wikiRoot == "" {
		wikiRoot = ".wiki"
	}
	return &Migrator{
		repoRoot:   repoRoot,
		wikiRoot:   filepath.Join(repoRoot, wikiRoot),
		plexiumDir: filepath.Join(repoRoot, ".plexium"),
	}
}

// Migrate runs pending migrations up to the target version.
// If targetVersion is 0, migrate to the latest.
func (m *Migrator) Migrate(targetVersion int, dryRun bool) (*MigrationResult, error) {
	result := &MigrationResult{
		DryRun: dryRun,
	}

	// 1. Read current schema version from _schema.md
	current, err := m.readSchemaVersion()
	if err != nil {
		return nil, fmt.Errorf("reading schema version: %w", err)
	}
	result.CurrentVersion = current

	// 2. Find migration scripts in .plexium/migrations/
	migrations, err := m.findMigrations()
	if err != nil {
		return nil, fmt.Errorf("finding migrations: %w", err)
	}

	if len(migrations) == 0 {
		result.TargetVersion = current
		return result, nil
	}

	// 3. Determine target version
	if targetVersion == 0 {
		// Latest = highest migration number
		targetVersion = migrations[len(migrations)-1].Number
	}
	result.TargetVersion = targetVersion

	// 4. Filter to pending migrations (greater than current, up to target)
	var pending []Migration
	for _, mg := range migrations {
		if mg.Number > current && mg.Number <= targetVersion {
			pending = append(pending, mg)
		}
	}

	if len(pending) == 0 {
		return result, nil
	}

	// 5. Apply each migration in order
	for _, mg := range pending {
		if dryRun {
			fmt.Printf("[dry-run] Would apply migration %d: %s\n", mg.Number, mg.Name)
			result.Applied = append(result.Applied, mg)
			continue
		}

		if err := m.applyMigration(mg); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("migration %d (%s): %v", mg.Number, mg.Name, err))
			break
		}
		result.Applied = append(result.Applied, mg)
	}

	// 6. Update schema version in _schema.md
	if !dryRun && len(result.Applied) > 0 && len(result.Errors) == 0 {
		lastApplied := result.Applied[len(result.Applied)-1].Number
		if err := m.updateSchemaVersion(lastApplied); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("updating schema version: %v", err))
		}
	}

	return result, nil
}

// readSchemaVersion reads the version from .wiki/_schema.md.
// Looks for a line like: "version: 1" or "schema-version: 1".
func (m *Migrator) readSchemaVersion() (int, error) {
	schemaPath := filepath.Join(m.wikiRoot, "_schema.md")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // no schema = version 0
		}
		return 0, err
	}

	re := regexp.MustCompile(`(?i)^[#\s]*schema[-\s]?version\s*:\s*(\d+)`)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			return strconv.Atoi(matches[1])
		}
	}

	// Try alternate format: "version: 1" in frontmatter
	re2 := regexp.MustCompile(`^version:\s*(\d+)`)
	scanner = bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if matches := re2.FindStringSubmatch(line); len(matches) > 1 {
			return strconv.Atoi(matches[1])
		}
	}

	return 1, nil // default version 1 if schema exists but no version found
}

// updateSchemaVersion writes the new version to _schema.md.
func (m *Migrator) updateSchemaVersion(version int) error {
	schemaPath := filepath.Join(m.wikiRoot, "_schema.md")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return err
	}

	content := string(data)

	// Replace existing schema-version line
	re := regexp.MustCompile(`(?i)(^[#\s]*schema[-\s]?version\s*:\s*)\d+`)
	if re.MatchString(content) {
		content = re.ReplaceAllString(content, fmt.Sprintf("${1}%d", version))
	} else {
		// Add version line after frontmatter
		re2 := regexp.MustCompile(`(?m)^version:\s*\d+`)
		if re2.MatchString(content) {
			content = re2.ReplaceAllString(content, fmt.Sprintf("version: %d", version))
		} else {
			// Prepend to file
			content = fmt.Sprintf("schema-version: %d\n%s", version, content)
		}
	}

	return os.WriteFile(schemaPath, []byte(content), 0644)
}

// findMigrations discovers migration scripts in .plexium/migrations/.
func (m *Migrator) findMigrations() ([]Migration, error) {
	migrationsDir := filepath.Join(m.plexiumDir, "migrations")

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var migrations []Migration
	re := regexp.MustCompile(`^(\d+)_(.+)\.(sh|sql|go)$`)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := re.FindStringSubmatch(entry.Name())
		if len(matches) < 3 {
			continue
		}
		num, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		migrations = append(migrations, Migration{
			Number:  num,
			Name:    matches[2],
			Path:    filepath.Join(migrationsDir, entry.Name()),
			Version: num,
		})
	}

	// Sort by number
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Number < migrations[j].Number
	})

	return migrations, nil
}

// applyMigration runs a migration script.
func (m *Migrator) applyMigration(mg Migration) error {
	ext := filepath.Ext(mg.Path)
	switch ext {
	case ".sh":
		return m.runShellMigration(mg.Path)
	case ".sql":
		return fmt.Errorf("SQL migrations not yet supported (migration %d)", mg.Number)
	case ".go":
		return fmt.Errorf("Go migrations not yet supported (migration %d)", mg.Number)
	default:
		return fmt.Errorf("unknown migration type: %s", ext)
	}
}

// runShellMigration executes a shell migration script.
func (m *Migrator) runShellMigration(path string) error {
	cmd := exec.Command("bash", path)
	cmd.Dir = m.repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
