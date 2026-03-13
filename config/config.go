package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// Config represents .go-trace.yaml.
type Config struct {
	Instrument InstrumentConfig `yaml:"instrument"`
	Analysis   AnalysisConfig   `yaml:"analysis"`
}

// AnalysisConfig controls the analytics view behavior.
type AnalysisConfig struct {
	MatchingGroups []string `yaml:"matching_groups"`
}

// InstrumentConfig controls which files are instrumented.
type InstrumentConfig struct {
	Include []string `yaml:"include"`
}

// Default returns a config that instruments all Go files.
func Default() Config {
	return Config{
		Instrument: InstrumentConfig{
			Include: []string{"**/*.go"},
		},
	}
}

// Load reads and parses a .go-trace.yaml file.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if len(cfg.Instrument.Include) == 0 {
		cfg.Instrument.Include = Default().Instrument.Include
	}

	return cfg, nil
}

// Match reports whether the given relative file path matches
// any of the include patterns.
func (c Config) Match(path string) bool {
	for _, pattern := range c.Instrument.Include {
		matched, err := doublestar.PathMatch(pattern, path)
		if err == nil && matched {
			return true
		}
	}
	return false
}
