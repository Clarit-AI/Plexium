package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Clarit-AI/Plexium/internal/capabilityprofile"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config represents the Plexium configuration.
// See docs/architecture/core-architecture.md §7 for the full schema.
type Config struct {
	Version        int            `yaml:"version"`
	Repo           Repo           `yaml:"repo"`
	Sources        Sources        `yaml:"sources"`
	Agents         Agents         `yaml:"agents"`
	Wiki           Wiki           `yaml:"wiki"`
	Taxonomy       Taxonomy       `yaml:"taxonomy"`
	Publish        Publish        `yaml:"publish"`
	Sync           Sync           `yaml:"sync"`
	Enforcement    Enforcement    `yaml:"enforcement"`
	Integrations   Integrations   `yaml:"integrations"`
	Reports        Reports        `yaml:"reports"`
	GitHubWiki     GitHubWiki     `yaml:"githubWiki"`
	Sensitivity    Sensitivity    `yaml:"sensitivity"`
	AssistiveAgent AssistiveAgent `yaml:"assistiveAgent"`
	Daemon         DaemonConfig   `yaml:"daemon"`
	Retry          RetryConfig    `yaml:"retry"`
}

// AssistiveAgent configures the LLM provider cascade for wiki maintenance tasks.
type AssistiveAgent struct {
	Enabled   bool             `yaml:"enabled"`
	Providers []ProviderConfig `yaml:"providers"`
	Budget    BudgetConfig     `yaml:"budget"`
}

type ProviderConfig struct {
	Name              string `yaml:"name"`
	Enabled           bool   `yaml:"enabled"`
	Type              string `yaml:"type"` // ollama | openai-compatible | inherit
	Endpoint          string `yaml:"endpoint"`
	Model             string `yaml:"model"`
	APIKeyEnv         string `yaml:"apiKeyEnv"`
	CapabilityProfile string `yaml:"capabilityProfile"`
	RPM               int    `yaml:"requestsPerMinute"`
	RPD               int    `yaml:"requestsPerDay"`
	Tier              string `yaml:"tier"` // free | budget — only for openai-compatible type
}

type BudgetConfig struct {
	DailyUSD float64 `yaml:"dailyUSD"`
}

// DaemonConfig configures the autonomous maintenance loop.
type DaemonConfig struct {
	Enabled       bool        `yaml:"enabled"`
	PollInterval  int         `yaml:"pollInterval"` // seconds
	MaxConcurrent int         `yaml:"maxConcurrent"`
	Runner        string      `yaml:"runner"`      // claude | codex | gemini | noop
	RunnerModel   string      `yaml:"runnerModel"` // optional model override for runner
	Tracker       string      `yaml:"tracker"`     // github | linear | none
	Watches       WatchConfig `yaml:"watches"`
}

type WatchConfig struct {
	Staleness WatchEntry `yaml:"staleness"`
	Lint      WatchEntry `yaml:"lint"`
	Ingest    WatchEntry `yaml:"ingest"`
	Debt      WatchEntry `yaml:"debt"`
}

type WatchEntry struct {
	Enabled   bool   `yaml:"enabled"`
	Threshold string `yaml:"threshold"`
	Interval  string `yaml:"interval"`
	WatchDir  string `yaml:"watchDir"`
	MaxDebt   int    `yaml:"maxDebt"`
	Action    string `yaml:"action"` // auto-sync | auto-fix | auto-ingest | create-issue | log-only
}

// RetryConfig configures exponential backoff for transient failures.
type RetryConfig struct {
	MaxAttempts       int     `yaml:"maxAttempts"`
	InitialDelayMs    int     `yaml:"initialDelayMs"`
	BackoffMultiplier float64 `yaml:"backoffMultiplier"`
	MaxDelayMs        int     `yaml:"maxDelayMs"`
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
	Home    string `yaml:"home"`
	Sidebar string `yaml:"sidebar"`
	Footer  string `yaml:"footer"`
	Log     string `yaml:"log"`
	Index   string `yaml:"index"`
	Schema  string `yaml:"schema"`
}

type Taxonomy struct {
	Sections     []string `yaml:"sections"`
	AutoClassify bool     `yaml:"autoClassify"`
}

type Publish struct {
	Branch                 string `yaml:"branch"`
	Message                string `yaml:"message"`
	AutoPush               bool   `yaml:"autoPush"`
	PreserveUnmanagedPages bool   `yaml:"preserveUnmanagedPages"`
	ManagedMarkerComment   bool   `yaml:"managedMarkerComment"`
}

type Sync struct {
	Mode                 string   `yaml:"mode"` // incremental | full
	AutoSync             bool     `yaml:"autoSync"`
	OnCommit             bool     `yaml:"onCommit"`
	OnPush               bool     `yaml:"onPush"`
	RewriteHomeOnSync    bool     `yaml:"rewriteHomeOnSync"`
	RewriteSidebarOnSync bool     `yaml:"rewriteSidebarOnSync"`
	Idempotent           bool     `yaml:"idempotent"`
	Exclude              []string `yaml:"exclude"`
}

type Enforcement struct {
	PreCommitHook bool   `yaml:"preCommitHook"`
	CICheck       bool   `yaml:"ciCheck"`
	MementoGate   bool   `yaml:"mementoGate"`
	Strictness    string `yaml:"strictness"` // strict | moderate | advisory
	BlockOnDebt   bool   `yaml:"blockOnDebt"`
	DebtThreshold int    `yaml:"debtThreshold"`
}

type Integrations struct {
	LLMProvider string `yaml:"llmProvider"`
	Memento     bool   `yaml:"memento"`
	Beads       bool   `yaml:"beads"`
	PageIndex   bool   `yaml:"pageindex"`
	Obsidian    bool   `yaml:"obsidian"`
}

type Reports struct {
	Bootstrap []string `yaml:"bootstrap"` // markdown, json
	Sync      []string `yaml:"sync"`
	Lint      []string `yaml:"lint"`
	Format    string   `yaml:"format"` // json | markdown | both
	OutputDir string   `yaml:"outputDir"`
}

type GitHubWiki struct {
	Enabled   bool     `yaml:"enabled"`
	Submodule bool     `yaml:"submodule"`
	Publish   []string `yaml:"publish"`
	Exclude   []string `yaml:"exclude"`
}

type Sensitivity struct {
	Rules        string   `yaml:"rules"`
	NeverPublish []string `yaml:"neverPublish"`
	MaxFileSize  int64    `yaml:"maxFileSize"`
	ExcludeExts  []string `yaml:"excludeExtensions"`
}

// envBindings maps config keys to their environment variable overrides.
// Viper's AutomaticEnv only works with explicit Bind calls when using Unmarshal.
var envBindings = map[string]string{
	"wiki.root":              "PLEXIUM_WIKI_ROOT",
	"wiki.home":              "PLEXIUM_WIKI_HOME",
	"wiki.sidebar":           "PLEXIUM_WIKI_SIDEBAR",
	"wiki.footer":            "PLEXIUM_WIKI_FOOTER",
	"wiki.log":               "PLEXIUM_WIKI_LOG",
	"wiki.index":             "PLEXIUM_WIKI_INDEX",
	"wiki.schema":            "PLEXIUM_WIKI_SCHEMA",
	"repo.defaultBranch":     "PLEXIUM_REPO_DEFAULT_BRANCH",
	"repo.wikiEnabled":       "PLEXIUM_REPO_WIKI_ENABLED",
	"sources.include":        "PLEXIUM_SOURCES_INCLUDE",
	"sources.exclude":        "PLEXIUM_SOURCES_EXCLUDE",
	"agents.strictness":      "PLEXIUM_AGENTS_STRICTNESS",
	"sync.mode":              "PLEXIUM_SYNC_MODE",
	"enforcement.strictness": "PLEXIUM_ENFORCEMENT_STRICTNESS",
	"githubWiki.enabled":     "PLEXIUM_GITHUB_WIKI_ENABLED",
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

	// Bind specific env vars so they work with Unmarshal
	for key, envVar := range envBindings {
		_ = v.BindEnv(key, envVar)
	}

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

// Save writes the config back to disk in YAML form.
func Save(configPath string, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// SaveToDir writes .plexium/config.yml under the provided repository root.
func SaveToDir(dir string, cfg *Config) error {
	return Save(filepath.Join(dir, ".plexium", "config.yml"), cfg)
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
	for i := range c.AssistiveAgent.Providers {
		profile := capabilityprofile.Normalize(c.AssistiveAgent.Providers[i].CapabilityProfile)
		if profile == "" {
			return fmt.Errorf("assistiveAgent.providers[%d].capabilityProfile %q is invalid (expected one of: balanced, constrained-local, frontier-large-context)", i, c.AssistiveAgent.Providers[i].CapabilityProfile)
		}
		c.AssistiveAgent.Providers[i].CapabilityProfile = profile
	}
	return nil
}

// Getwd returns the current working directory
func Getwd() (string, error) {
	return os.Getwd()
}
