package server

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
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

	// Compaction configures logarithmic retention policy for the inactive log.
	// If nil, compaction is disabled (all data retained).
	Compaction *CompactionConfig `tony:"field=compaction"`

	// Storage configures the storage layer itself.
	// If nil, storage defaults apply.
	Storage *StorageConfig `tony:"field=storage"`
}

// StorageConfig configures the storage layer.
//
//tony:schemagen=storage-config
type StorageConfig struct {
	// Durability controls when a commit's log record is forced to stable storage:
	//
	//	os   — (default) acknowledge a commit once its record is written to the OS
	//	       page cache. No fsync on the commit path, so a machine crash — as
	//	       opposed to a process crash, which the page cache survives — loses
	//	       whatever the OS had not yet flushed.
	//	sync — fsync each commit's record before it is indexed, so a commit that has
	//	       been acknowledged is on stable storage. Costs one fsync per commit.
	//
	// Either way a lost tail costs commits, never their identity: the commit
	// watermark is reconciled against the log on open, so a number the log already
	// holds is never reissued.
	Durability string `tony:"field=durability"`
}

// ToStorageDurability maps the configured name to a storage.Durability. A nil
// section, or an empty name, means the storage default.
func (c *StorageConfig) ToStorageDurability() (storage.Durability, error) {
	if c == nil || c.Durability == "" {
		return storage.DurabilityOS, nil
	}
	switch c.Durability {
	case "os":
		return storage.DurabilityOS, nil
	case "sync":
		return storage.DurabilitySync, nil
	default:
		return storage.DurabilityOS, fmt.Errorf("unknown storage durability %q: want %q or %q",
			c.Durability, "os", "sync")
	}
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

// CompactionConfig configures logarithmic retention policy for compaction.
// Implements exponential time bucketing where older data is kept at coarser granularity.
//
//tony:schemagen=compaction-config
type CompactionConfig struct {
	// Cutoff is the duration within which all patches are kept for accurate historical reads.
	// Beyond this cutoff, history degrades to snapshot granularity.
	// Default: 1h
	Cutoff time.Duration `tony:"field=cutoff"`

	// BaseInterval is the snapshot retention interval for the first tier after cutoff.
	// Default: 1h
	BaseInterval time.Duration `tony:"field=baseInterval"`

	// SlotsPerTier is the number of snapshots to keep in each time tier.
	// Default: 8
	SlotsPerTier int `tony:"field=slotsPerTier"`

	// Multiplier is the factor by which each tier's interval increases.
	// Tier N has interval = BaseInterval * Multiplier^N
	// Default: 2
	Multiplier int `tony:"field=multiplier"`

	// GracePeriod is how long to wait for active readers to finish after swap.
	// After this timeout, old file is deleted and lingering readers will error.
	// Default: 5s
	GracePeriod time.Duration `tony:"field=gracePeriod"`
}

// ToStorageConfig converts to storage.CompactionConfig.
func (c *CompactionConfig) ToStorageConfig() *storage.CompactionConfig {
	if c == nil {
		return nil
	}
	cfg := storage.DefaultCompactionConfig()

	if c.Cutoff > 0 {
		cfg.Cutoff = c.Cutoff
	}
	if c.BaseInterval > 0 {
		cfg.BaseInterval = c.BaseInterval
	}
	if c.SlotsPerTier > 0 {
		cfg.SlotsPerTier = c.SlotsPerTier
	}
	if c.Multiplier > 0 {
		cfg.Multiplier = c.Multiplier
	}
	if c.GracePeriod > 0 {
		cfg.GracePeriod = c.GracePeriod
	}
	return cfg
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

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
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

// Validate checks the configuration for errors. Called by LoadConfig, so a file
// that names something logd does not understand is rejected rather than run.
func (c *Config) Validate() error {
	// A misspelled durability must not fall back to the default: an operator who
	// wrote "fsync" and silently got page-cache writes has the opposite of what
	// they asked for, and would not find out until a crash.
	if _, err := c.Storage.ToStorageDurability(); err != nil {
		return err
	}
	return nil
}
