package server

import (
	"fmt"
	"os"
)

// Config represents the logd server configuration file structure.
// Designed for extensibility - new sections can be added without breaking existing configs.
//
//tony:schemagen=config
type Config struct {
	// Snapshot configures automatic snapshotting behavior.
	Snapshot *SnapshotConfig `tony:"field=snapshot"`
}

// SnapshotConfig configures when automatic snapshots are triggered.
//
//tony:schemagen=snapshot-config
type SnapshotConfig struct {
	// MaxCommits triggers a snapshot after this many commits since the last snapshot.
	// Zero or negative means disabled.
	MaxCommits int64 `tony:"field=maxCommits"`

	// MaxBytes triggers a snapshot when the active log exceeds this size in bytes.
	// Zero or negative means disabled.
	MaxBytes int64 `tony:"field=maxBytes"`
}

// LoadConfig loads a configuration file in Tony format.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := &Config{}
	if err := cfg.FromTony(data); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return cfg, nil
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Snapshot: &SnapshotConfig{
			MaxCommits: 1000, // Snapshot every 1000 commits by default
		},
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	// Currently no validation rules, but this is where they would go
	return nil
}
