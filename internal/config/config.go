package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config represents the Plexium configuration
type Config struct {
	Version      int           `yaml:"version"`
	Repo         Repo          `yaml:"repo"`
	Sources      Sources       `yaml:"sources"`
	Agents       Agents        `yaml:"agents"`
	Wiki         Wiki          `yaml:"wiki"`
	Taxonomy     Taxonomy      `yaml:"taxonomy"`
	Publish      Publish       `yaml:"publish"`
	Sync         Sync          `yaml:"sync"`
	Enforcement  Enforcement   `yaml:"enforcement"`
	Integrations Integrations  `yaml:"integrations"`
	Reports      Reports       `yaml:"reports"`
	GitHubWiki   GitHubWiki    `yaml:"githubWiki"`
	Sensitivity  Sensitivity   `yaml:"sensitivity"`
}

type Repo struct {
	DefaultBranch string `yaml:"defaultBranch"`
	WikiEnabled   bool   `yaml:"wikiEnabled"`
}

type Sources struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

type Agents struct {
	Adapters   []string `yaml:"adapters"`
	Strictness string   `yaml:"strictness"` // strict | moderate | advisory
}

type Wiki struct {
	Root    string `yaml:"root"`
	Title   string `yaml:"title"`
	Home    string `yaml:"home"`
	NavFile string `yaml:"navFile"`
}

type Taxonomy struct {
	Modules   []string `yaml:"modules"`
	Decisions []string `yaml:"decisions"`
	Concepts  []string `yaml:"concepts"`
}

type Publish struct {
	Branch   string `yaml:"branch"`
	Message  string `yaml:"message"`
	AutoPush bool   `yaml:"autoPush"`
}

type Sync struct {
	AutoSync  bool     `yaml:"autoSync"`
	OnCommit  bool     `yaml:"onCommit"`
	OnPush    bool     `yaml:"onPush"`
	Exclude   []string `yaml:"exclude"`
}

type Enforcement struct {
	Strictness    string `yaml:"strictness"` // strict | moderate | advisory
	BlockOnDebt   bool   `yaml:"blockOnDebt"`
	DebtThreshold int    `yaml:"debtThreshold"`
}

type Integrations struct {
	LLMProvider string `yaml:"llmProvider"`
	Memento     bool   `yaml:"memento"`
	Beads       bool   `yaml:"beads"`
}

type Reports struct {
	Bootstrap []string `yaml:"bootstrap"` // markdown, json
	Sync      []string `yaml:"sync"`
	Lint      []string `yaml:"lint"`
}

type GitHubWiki struct {
	Enabled bool   `yaml:"enabled"`
	Subtree string `yaml:"subtree"`
}

type Sensitivity struct {
	MaxFileSize int64  `yaml:"maxFileSize"`
	ExcludeExts []string `yaml:"excludeExtensions"`
}

// Load reads configuration from .plexium/config.yml with environment variable overrides
func Load(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = ".plexium/config.yml"
	}

	v := viper.New()

	// Set config file path
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	// Enable environment variable overrides
	v.AutomaticEnv()
	v.SetEnvPrefix("PLEXIUM")

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil, fmt.Errorf("config file not found: %s (or set PLEXIUM_CONFIG)", configPath)
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	// Validate required fields
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return &cfg, nil
}

// LoadFromDir loads config from a specific directory
func LoadFromDir(dir string) (*Config, error) {
	configPath := filepath.Join(dir, ".plexium", "config.yml")
	return Load(configPath)
}

// MustLoad loads config or panics
func MustLoad(configPath string) *Config {
	cfg, err := Load(configPath)
	if err != nil {
		panic(err)
	}
	return cfg
}

// Validate checks that required config fields are present
func (c *Config) Validate() error {
	if c.Version == 0 {
		return fmt.Errorf("version is required")
	}
	if c.Wiki.Root == "" {
		return fmt.Errorf("wiki.root is required")
	}
	if c.Sources.Include == nil {
		return fmt.Errorf("sources.include is required")
	}
	return nil
}

// Getwd returns the current working directory
func Getwd() (string, error) {
	return os.Getwd()
}
