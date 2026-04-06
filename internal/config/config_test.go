package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".plexium", "config.yml")
	err := os.MkdirAll(filepath.Dir(configPath), 0755)
	require.NoError(t, err)

	validConfig := `
version: 1
wiki:
  root: .wiki
sources:
  include:
    - "**/*.go"
    - "docs/**/*.md"
`
	err = os.WriteFile(configPath, []byte(validConfig), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)
	assert.Equal(t, 1, cfg.Version)
	assert.Equal(t, ".wiki", cfg.Wiki.Root)
	assert.Contains(t, cfg.Sources.Include, "**/*.go")
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yml")
	assert.Error(t, err)
}

func TestLoad_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	err := os.WriteFile(configPath, []byte("invalid: yaml: content:"), 0644)
	require.NoError(t, err)

	_, err = Load(configPath)
	assert.Error(t, err)
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			cfg: Config{
				Version: 1,
				Wiki:    Wiki{Root: ".wiki"},
				Sources: Sources{Include: []string{"**/*.go"}},
			},
			wantErr: false,
		},
		{
			name: "missing version",
			cfg: Config{
				Wiki:    Wiki{Root: ".wiki"},
				Sources: Sources{Include: []string{"**/*.go"}},
			},
			wantErr: true,
			errMsg:  "version is required",
		},
		{
			name: "missing wiki root",
			cfg: Config{
				Version: 1,
				Sources: Sources{Include: []string{"**/*.go"}},
			},
			wantErr: true,
			errMsg:  "wiki.root is required",
		},
		{
			name: "missing sources",
			cfg: Config{
				Version: 1,
				Wiki:    Wiki{Root: ".wiki"},
			},
			wantErr: true,
			errMsg:  "sources.include is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}