package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/capabilityprofile"
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
  home: Home.md
  sidebar: _Sidebar.md
  footer: _Footer.md
  log: _log.md
  index: _index.md
  schema: _schema.md
sources:
  include:
    - "**/*.go"
    - "docs/**/*.md"
taxonomy:
  sections:
    - Architecture
    - Modules
    - Decisions
  autoClassify: true
sync:
  mode: incremental
  idempotent: true
enforcement:
  preCommitHook: true
  ciCheck: true
  strictness: moderate
reports:
  format: both
  outputDir: .plexium/reports/
githubWiki:
  enabled: true
  submodule: true
`
	err = os.WriteFile(configPath, []byte(validConfig), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)
	assert.Equal(t, 1, cfg.Version)
	assert.Equal(t, ".wiki", cfg.Wiki.Root)
	assert.Equal(t, "Home.md", cfg.Wiki.Home)
	assert.Equal(t, "_Sidebar.md", cfg.Wiki.Sidebar)
	assert.Equal(t, "_log.md", cfg.Wiki.Log)
	assert.Equal(t, "_index.md", cfg.Wiki.Index)
	assert.Equal(t, "_schema.md", cfg.Wiki.Schema)
	assert.Contains(t, cfg.Sources.Include, "**/*.go")
	assert.True(t, cfg.Taxonomy.AutoClassify)
	assert.Equal(t, "incremental", cfg.Sync.Mode)
	assert.True(t, cfg.Sync.Idempotent)
	assert.True(t, cfg.Enforcement.PreCommitHook)
	assert.True(t, cfg.GitHubWiki.Enabled)
	assert.True(t, cfg.GitHubWiki.Submodule)
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
		name                      string
		cfg                       Config
		wantErr                   bool
		errMsg                    string
		expectedCapabilityProfile string
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
			name: "valid capability profile",
			cfg: Config{
				Version: 1,
				Wiki:    Wiki{Root: ".wiki"},
				Sources: Sources{Include: []string{"**/*.go"}},
				AssistiveAgent: AssistiveAgent{
					Providers: []ProviderConfig{{
						Name:              "openrouter",
						CapabilityProfile: capabilityprofile.FrontierLargeContext,
					}},
				},
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
		{
			name: "invalid capability profile",
			cfg: Config{
				Version: 1,
				Wiki:    Wiki{Root: ".wiki"},
				Sources: Sources{Include: []string{"**/*.go"}},
				AssistiveAgent: AssistiveAgent{
					Providers: []ProviderConfig{{CapabilityProfile: "extreme"}},
				},
			},
			wantErr: true,
			errMsg:  "capabilityProfile",
		},
		{
			name: "valid capability profile is normalized",
			cfg: Config{
				Version: 1,
				Wiki:    Wiki{Root: ".wiki"},
				Sources: Sources{Include: []string{"**/*.go"}},
				AssistiveAgent: AssistiveAgent{
					Providers: []ProviderConfig{{CapabilityProfile: " Frontier-Large-Context "}},
				},
			},
			wantErr:                   false,
			expectedCapabilityProfile: "frontier-large-context",
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
				if tt.expectedCapabilityProfile != "" {
					assert.Equal(t, tt.expectedCapabilityProfile, tt.cfg.AssistiveAgent.Providers[0].CapabilityProfile)
				}
			}
		})
	}
}
