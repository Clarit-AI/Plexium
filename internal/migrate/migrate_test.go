package migrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrator_NoMigrations(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	plexiumDir := filepath.Join(tmpDir, ".plexium")
	os.MkdirAll(wikiDir, 0755)
	os.MkdirAll(plexiumDir, 0755)

	// Create _schema.md with version 1
	os.WriteFile(filepath.Join(wikiDir, "_schema.md"), []byte("schema-version: 1\n"), 0644)

	m := NewMigrator(tmpDir, ".wiki")
	result, err := m.Migrate(0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CurrentVersion != 1 {
		t.Errorf("current version = %d, want 1", result.CurrentVersion)
	}
	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied, got %d", len(result.Applied))
	}
}

func TestMigrator_NoSchemaFile(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	plexiumDir := filepath.Join(tmpDir, ".plexium")
	os.MkdirAll(wikiDir, 0755)
	os.MkdirAll(filepath.Join(plexiumDir, "migrations"), 0755)

	m := NewMigrator(tmpDir, ".wiki")
	result, err := m.Migrate(0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CurrentVersion != 0 {
		t.Errorf("current version = %d, want 0", result.CurrentVersion)
	}
}

func TestMigrator_ApplyShellMigration(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	plexiumDir := filepath.Join(tmpDir, ".plexium")
	migrationsDir := filepath.Join(plexiumDir, "migrations")
	os.MkdirAll(wikiDir, 0755)
	os.MkdirAll(migrationsDir, 0755)

	// Create _schema.md with version 0
	os.WriteFile(filepath.Join(wikiDir, "_schema.md"), []byte("schema-version: 0\n"), 0644)

	// Create a simple migration that creates a file
	migrationScript := "#!/bin/bash\necho migrated > " + filepath.Join(wikiDir, "test-output.txt") + "\n"
	os.WriteFile(filepath.Join(migrationsDir, "001_add_test_field.sh"), []byte(migrationScript), 0755)

	m := NewMigrator(tmpDir, ".wiki")
	result, err := m.Migrate(0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("expected 1 applied, got %d", len(result.Applied))
	}
	if result.Applied[0].Number != 1 {
		t.Errorf("migration number = %d, want 1", result.Applied[0].Number)
	}

	// Verify the migration ran
	data, err := os.ReadFile(filepath.Join(wikiDir, "test-output.txt"))
	if err != nil {
		t.Fatalf("reading migration output: %v", err)
	}
	if string(data) != "migrated\n" {
		t.Errorf("migration output = %q, want %q", string(data), "migrated\n")
	}

	// Verify schema version was updated
	schemaData, _ := os.ReadFile(filepath.Join(wikiDir, "_schema.md"))
	schemaContent := string(schemaData)
	if schemaContent != "schema-version: 1\n" {
		t.Errorf("schema version not updated: %q", schemaContent)
	}
}

func TestMigrator_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	plexiumDir := filepath.Join(tmpDir, ".plexium")
	migrationsDir := filepath.Join(plexiumDir, "migrations")
	os.MkdirAll(wikiDir, 0755)
	os.MkdirAll(migrationsDir, 0755)

	os.WriteFile(filepath.Join(wikiDir, "_schema.md"), []byte("schema-version: 0\n"), 0644)

	migrationScript := "#!/bin/bash\necho should-not-run > " + filepath.Join(wikiDir, "should-not-exist.txt") + "\n"
	os.WriteFile(filepath.Join(migrationsDir, "001_dry_run_test.sh"), []byte(migrationScript), 0755)

	m := NewMigrator(tmpDir, ".wiki")
	result, err := m.Migrate(0, true) // dry-run = true
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.DryRun {
		t.Error("expected dryRun=true")
	}
	if len(result.Applied) != 1 {
		t.Errorf("expected 1 applied in dry-run, got %d", len(result.Applied))
	}

	// Verify migration did NOT actually run
	if _, err := os.Stat(filepath.Join(wikiDir, "should-not-exist.txt")); !os.IsNotExist(err) {
		t.Error("expected migration NOT to run in dry-run mode")
	}

	// Schema version should NOT be updated
	schemaData, _ := os.ReadFile(filepath.Join(wikiDir, "_schema.md"))
	if string(schemaData) != "schema-version: 0\n" {
		t.Error("expected schema version to remain 0 in dry-run")
	}
}

func TestMigrator_TargetVersion(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	plexiumDir := filepath.Join(tmpDir, ".plexium")
	migrationsDir := filepath.Join(plexiumDir, "migrations")
	os.MkdirAll(wikiDir, 0755)
	os.MkdirAll(migrationsDir, 0755)

	os.WriteFile(filepath.Join(wikiDir, "_schema.md"), []byte("schema-version: 0\n"), 0644)

	// Create two migrations
	os.WriteFile(filepath.Join(migrationsDir, "001_first.sh"), []byte("#!/bin/bash\ntrue\n"), 0755)
	os.WriteFile(filepath.Join(migrationsDir, "002_second.sh"), []byte("#!/bin/bash\ntrue\n"), 0755)

	m := NewMigrator(tmpDir, ".wiki")
	result, err := m.Migrate(1, false) // target version 1
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Errorf("expected 1 applied (up to v1), got %d", len(result.Applied))
	}
	if result.TargetVersion != 1 {
		t.Errorf("target version = %d, want 1", result.TargetVersion)
	}
}

func TestMigrator_AlreadyApplied(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	plexiumDir := filepath.Join(tmpDir, ".plexium")
	migrationsDir := filepath.Join(plexiumDir, "migrations")
	os.MkdirAll(wikiDir, 0755)
	os.MkdirAll(migrationsDir, 0755)

	os.WriteFile(filepath.Join(wikiDir, "_schema.md"), []byte("schema-version: 2\n"), 0644)

	// Create migrations that are already applied
	os.WriteFile(filepath.Join(migrationsDir, "001_old.sh"), []byte("#!/bin/bash\ntrue\n"), 0755)
	os.WriteFile(filepath.Join(migrationsDir, "002_also_old.sh"), []byte("#!/bin/bash\ntrue\n"), 0755)

	m := NewMigrator(tmpDir, ".wiki")
	result, err := m.Migrate(0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied (already at v2), got %d", len(result.Applied))
	}
}

func TestReadSchemaVersion_VersionField(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	os.MkdirAll(wikiDir, 0755)

	content := "---\nversion: 3\n---\n\n# Wiki Schema\n"
	os.WriteFile(filepath.Join(wikiDir, "_schema.md"), []byte(content), 0644)

	m := NewMigrator(tmpDir, ".wiki")
	version, err := m.readSchemaVersion()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != 3 {
		t.Errorf("version = %d, want 3", version)
	}
}

func TestFindMigrations_Sorting(t *testing.T) {
	tmpDir := t.TempDir()
	migrationsDir := filepath.Join(tmpDir, ".plexium", "migrations")
	os.MkdirAll(migrationsDir, 0755)

	// Create migrations in non-sorted order
	os.WriteFile(filepath.Join(migrationsDir, "003_third.sh"), []byte("#!/bin/bash\ntrue\n"), 0755)
	os.WriteFile(filepath.Join(migrationsDir, "001_first.sh"), []byte("#!/bin/bash\ntrue\n"), 0755)
	os.WriteFile(filepath.Join(migrationsDir, "002_second.sh"), []byte("#!/bin/bash\ntrue\n"), 0755)

	m := NewMigrator(tmpDir, ".wiki")
	migrations, err := m.findMigrations()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(migrations) != 3 {
		t.Fatalf("expected 3 migrations, got %d", len(migrations))
	}
	for i, mg := range migrations {
		if mg.Number != i+1 {
			t.Errorf("migration[%d].Number = %d, want %d", i, mg.Number, i+1)
		}
	}
}
