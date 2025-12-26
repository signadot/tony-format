package server

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// Config represents the logd server configuration file structure.
// Designed for extensibility - new sections can be added without breaking existing configs.
//
//tony:schemagen=config
type Config struct {
	// Schema is the Tony schema node that defines data model constraints.
	// Use !tovalue.file to load from a file: schema: !tovalue.file path/to/schema.tony
	// The schema is used to identify auto-id fields (tagged with !logd-auto-id).
	// If nil, auto-id generation is disabled.
	Schema *ir.Node `tony:"field=schema"`

	// Snapshot configures automatic snapshotting behavior.
	Snapshot *SnapshotConfig `tony:"field=snapshot"`

	// Tx configures transaction behavior.
	Tx *TxConfig `tony:"field=tx"`
}

// TxConfig configures transaction behavior.
//
//tony:schemagen=tx-config
type TxConfig struct {
	// Timeout is the maximum time to wait for all participants to join a transaction.
	// If not all participants join within this duration, the transaction is aborted
	// and waiting participants receive a timeout error.
	// Default: 1s
	Timeout time.Duration `tony:"field=timeout"`
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
// It uses tony.Tool to expand tags like !tovalue.file for loading schema files.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	node, err := parse.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Change to the config file's directory for relative path resolution
	origDir, _ := os.Getwd()
	configDir := filepath.Dir(path)
	if configDir != "" && configDir != "." {
		if err := os.Chdir(configDir); err != nil {
			return nil, fmt.Errorf("failed to change to config directory: %w", err)
		}
		defer os.Chdir(origDir)
	}

	// Expand tags like !tovalue.file using tony.Tool
	tool := tony.DefaultTool()
	expanded, err := tool.Run(node)
	if err != nil {
		return nil, fmt.Errorf("failed to expand config file: %w", err)
	}

	cfg := &Config{}
	if err := cfg.FromTonyIR(expanded); err != nil {
		return nil, fmt.Errorf("failed to convert config: %w", err)
	}

	return cfg, nil
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Snapshot: &SnapshotConfig{
			MaxCommits: 1000, // Snapshot every 1000 commits by default
		},
		Tx: &TxConfig{
			Timeout: 1 * time.Second, // Default transaction timeout
		},
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	// Currently no validation rules, but this is where they would go
	return nil
}
