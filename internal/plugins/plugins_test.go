package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectTechStack_TypeScript(t *testing.T) {
	dir := t.TempDir()

	// Create TypeScript files
	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte("{}"), 0644)

	stack := DetectTechStack(dir)
	if stack != TechStackTypeScript {
		t.Errorf("expected typescript, got %s", stack)
	}
}

func TestDetectTechStack_JavaScript(t *testing.T) {
	dir := t.TempDir()

	// Create JavaScript file only (no tsconfig)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644)

	stack := DetectTechStack(dir)
	if stack != TechStackJavaScript {
		t.Errorf("expected javascript, got %s", stack)
	}
}

func TestDetectTechStack_Python(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("requests==2.28.0"), 0644)

	stack := DetectTechStack(dir)
	if stack != TechStackPython {
		t.Errorf("expected python, got %s", stack)
	}
}

func TestDetectTechStack_Rust(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\""), 0644)

	stack := DetectTechStack(dir)
	if stack != TechStackRust {
		t.Errorf("expected rust, got %s", stack)
	}
}

func TestDetectTechStack_Go(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	stack := DetectTechStack(dir)
	if stack != TechStackGo {
		t.Errorf("expected go, got %s", stack)
	}
}

func TestDetectTechStack_Java(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project></project>"), 0644)

	stack := DetectTechStack(dir)
	if stack != TechStackJava {
		t.Errorf("expected java, got %s", stack)
	}
}

func TestDetectTechStack_Generic(t *testing.T) {
	dir := t.TempDir()

	// Empty directory
	stack := DetectTechStack(dir)
	if stack != TechStackGeneric {
		t.Errorf("expected generic, got %s", stack)
	}
}

func TestSchemaGenerator_Generate(t *testing.T) {
	dir := t.TempDir()

	gen := NewSchemaGenerator(dir)
	content, err := gen.Generate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if content == "" {
		t.Error("expected non-empty schema content")
	}

	// Should contain base schema content
	if !contains(content, "PLEXIUM SCHEMA v1") {
		t.Error("expected schema to contain PLEXIUM SCHEMA v1")
	}

	// Should contain tech stack examples
	if !contains(content, "Tech Stack Examples") {
		t.Error("expected schema to contain Tech Stack Examples section")
	}

	if !contains(content, "Generic project") {
		t.Error("expected schema to contain generic project notice")
	}
}

func TestSchemaGenerator_Generate_WithGo(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	gen := NewSchemaGenerator(dir)
	content, err := gen.Generate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(content, "Go. Key conventions") {
		t.Error("expected schema to contain Go conventions")
	}
}

func TestSchemaGenerator_Generate_WithTypeScript(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte("{}"), 0644)

	gen := NewSchemaGenerator(dir)
	content, err := gen.Generate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(content, "TypeScript. Key conventions") {
		t.Error("expected schema to contain TypeScript conventions")
	}
}

func TestGetAvailableAdapters(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, ".plexium", "plugins")

	// No plugins
	adapters := GetAvailableAdapters(dir)
	if len(adapters) != 0 {
		t.Errorf("expected 0 adapters, got %d", len(adapters))
	}

	// Create a plugin
	os.MkdirAll(filepath.Join(pluginsDir, "test-plugin"), 0755)
	os.WriteFile(filepath.Join(pluginsDir, "test-plugin", "plugin.sh"), []byte("#!/bin/bash\necho test"), 0755)

	adapters = GetAvailableAdapters(dir)
	if len(adapters) != 1 {
		t.Errorf("expected 1 adapter, got %d", len(adapters))
	}
	if adapters[0] != "test-plugin" {
		t.Errorf("expected test-plugin, got %s", adapters[0])
	}
}

func TestGetAvailableAdapters_NoPluginsDir(t *testing.T) {
	dir := t.TempDir()

	adapters := GetAvailableAdapters(dir)
	if len(adapters) != 0 {
		t.Errorf("expected 0 adapters, got %d", len(adapters))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
