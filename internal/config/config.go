package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Duration is a time.Duration that round-trips through YAML as a human-readable
// string (e.g. "3s", "200ms").
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	dur, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	d.Duration = dur
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return d.Duration.String(), nil
}

// DefaultConfig holds global scan settings applied when no profile overrides them.
type DefaultConfig struct {
	MaxConnPerHost      int      `yaml:"max_conn_per_host"`
	MaxParallelHosts    int      `yaml:"max_parallel_hosts"`
	Timeout             Duration `yaml:"timeout"`
	Delay               Duration `yaml:"delay"`
	Output              string   `yaml:"output"`
	UserAgent           string   `yaml:"user_agent"`
	Wordlists           []string `yaml:"wordlists"`
	FailStatusCodes     []int    `yaml:"fail_status_codes"`
	QuarantineThreshold int      `yaml:"quarantine_threshold"`
	SimilarityThreshold float64  `yaml:"similarity_threshold"`
}

// Profile holds per-program scan settings. Pointer fields are set only when
// explicitly present in the YAML, enabling accurate defaults merging.
type Profile struct {
	ScopeFile           string    `yaml:"scope_file"`
	Wordlists           []string  `yaml:"wordlists"`
	OpenAPIURL          string    `yaml:"openapi_url"`
	MaxConnPerHost      *int      `yaml:"max_conn_per_host"`
	MaxParallelHosts    *int      `yaml:"max_parallel_hosts"`
	Timeout             *Duration `yaml:"timeout"`
	Delay               *Duration `yaml:"delay"`
	Output              string    `yaml:"output"`
	UserAgent           string    `yaml:"user_agent"`
	FailStatusCodes     []int     `yaml:"fail_status_codes"`
	QuarantineThreshold *int      `yaml:"quarantine_threshold"`
	SimilarityThreshold *float64  `yaml:"similarity_threshold"`
	Report              string    `yaml:"report"`
}

// Config is the top-level structure of a ~/.kitestring.yaml file.
type Config struct {
	Version  int                `yaml:"version"`
	Defaults DefaultConfig      `yaml:"defaults"`
	Profiles map[string]Profile `yaml:"profiles"`
}

// ProbeConfig is the fully resolved scan configuration produced by merging
// a profile over global defaults. CLI flags override individual fields.
type ProbeConfig struct {
	MaxConnPerHost      int
	MaxParallelHosts    int
	Timeout             time.Duration
	Delay               time.Duration
	Output              string
	UserAgent           string
	Wordlists           []string
	FailStatusCodes     []int
	QuarantineThreshold int
	SimilarityThreshold float64
	ScopeFile           string
	OpenAPIURL          string
	Report              string
}

// Load reads and parses the YAML config file at path, expanding ~ in all path fields.
func Load(path string) (*Config, error) {
	expanded, err := expandTilde(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}
	expandConfigPaths(&cfg)
	return &cfg, nil
}

// Default returns a Config populated with sensible built-in defaults and no profiles.
func Default() *Config {
	return &Config{
		Version: 1,
		Defaults: DefaultConfig{
			MaxConnPerHost:      5,
			MaxParallelHosts:    50,
			Timeout:             Duration{10 * time.Second},
			Delay:               Duration{0},
			Output:              "pretty",
			UserAgent:           "Mozilla/5.0 (compatible; KiteString/1.0)",
			FailStatusCodes:     []int{400, 401, 404, 403, 501, 502},
			QuarantineThreshold: 10,
			SimilarityThreshold: 0.85,
		},
		Profiles: make(map[string]Profile),
	}
}

// ApplyProfile merges the named profile over global defaults and returns the
// resolved ProbeConfig. CLI flags should be applied on top of this by the caller.
func (c *Config) ApplyProfile(name string) (*ProbeConfig, error) {
	p, ok := c.Profiles[name]
	if !ok {
		available := c.ListProfiles()
		if len(available) == 0 {
			return nil, fmt.Errorf("profile %q not found (no profiles defined)", name)
		}
		return nil, fmt.Errorf("profile %q not found (available: %s)", name, strings.Join(available, ", "))
	}

	d := c.Defaults
	pc := &ProbeConfig{
		MaxConnPerHost:      d.MaxConnPerHost,
		MaxParallelHosts:    d.MaxParallelHosts,
		Timeout:             d.Timeout.Duration,
		Delay:               d.Delay.Duration,
		Output:              d.Output,
		UserAgent:           d.UserAgent,
		Wordlists:           append([]string(nil), d.Wordlists...),
		FailStatusCodes:     append([]int(nil), d.FailStatusCodes...),
		QuarantineThreshold: d.QuarantineThreshold,
		SimilarityThreshold: d.SimilarityThreshold,
	}

	// Apply profile overrides for each field that was explicitly set.
	if p.MaxConnPerHost != nil {
		pc.MaxConnPerHost = *p.MaxConnPerHost
	}
	if p.MaxParallelHosts != nil {
		pc.MaxParallelHosts = *p.MaxParallelHosts
	}
	if p.Timeout != nil {
		pc.Timeout = p.Timeout.Duration
	}
	if p.Delay != nil {
		pc.Delay = p.Delay.Duration
	}
	if p.Output != "" {
		pc.Output = p.Output
	}
	if p.UserAgent != "" {
		pc.UserAgent = p.UserAgent
	}
	if len(p.Wordlists) > 0 {
		pc.Wordlists = append([]string(nil), p.Wordlists...)
	}
	if len(p.FailStatusCodes) > 0 {
		pc.FailStatusCodes = append([]int(nil), p.FailStatusCodes...)
	}
	if p.QuarantineThreshold != nil {
		pc.QuarantineThreshold = *p.QuarantineThreshold
	}
	if p.SimilarityThreshold != nil {
		pc.SimilarityThreshold = *p.SimilarityThreshold
	}
	pc.ScopeFile = p.ScopeFile
	pc.OpenAPIURL = p.OpenAPIURL
	pc.Report = p.Report

	return pc, nil
}

// ListProfiles returns all profile names in sorted order.
func (c *Config) ListProfiles() []string {
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Save writes the config back to path using YAML marshaling.
func (c *Config) Save(path string) error {
	expanded, err := expandTilde(path)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(expanded, data, 0o644); err != nil {
		return fmt.Errorf("writing config file %q: %w", path, err)
	}
	return nil
}

// expandTilde replaces a leading ~ with the current user's home directory.
func expandTilde(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, path[1:]), nil
}

// expandConfigPaths expands ~ in all path-valued fields in the config.
func expandConfigPaths(cfg *Config) {
	for i, wl := range cfg.Defaults.Wordlists {
		if exp, err := expandTilde(wl); err == nil {
			cfg.Defaults.Wordlists[i] = exp
		}
	}
	if cfg.Profiles == nil {
		return
	}
	profiles := make(map[string]Profile, len(cfg.Profiles))
	for name, p := range cfg.Profiles {
		if p.ScopeFile != "" {
			if exp, err := expandTilde(p.ScopeFile); err == nil {
				p.ScopeFile = exp
			}
		}
		for i, wl := range p.Wordlists {
			if exp, err := expandTilde(wl); err == nil {
				p.Wordlists[i] = exp
			}
		}
		profiles[name] = p
	}
	cfg.Profiles = profiles
}
