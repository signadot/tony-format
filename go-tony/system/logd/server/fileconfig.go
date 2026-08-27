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
// Duration is a length of time written the way a person writes one: "1h", "30s",
// "500ms" — what time.ParseDuration reads and what time.Duration prints.
//
// time.Duration is an int64 and implements no text encoding of its own, so a config
// field declared as one was read as a NUMBER, and the number was NANOSECONDS: an
// hour was `cutoff: 3600000000000`, and `cutoff: 1h` was refused as "expected
// number, got String". Nobody writes a config that way and nobody reads one back.
//
// The codec machinery already honours encoding.TextMarshaler and TextUnmarshaler --
// it is how time.Time fields are written -- so saying it once here is the whole fix.
type Duration time.Duration

// MarshalText writes the duration the way time.Duration prints it.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

// UnmarshalText reads what time.ParseDuration reads.
func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

//tony:schemagen=tx-config
type TxConfig struct {
	// Timeout is the maximum time to wait for all participants to join a transaction.
	// If not all participants join within this duration, the transaction is aborted
	// and waiting participants receive a timeout error.
	// Default: 1s
	Timeout Duration `tony:"field=timeout"`
}

// SnapshotConfig configures when automatic snapshots are triggered.
//
// A snapshot is what bounds the cost of reading: without one, every read replays
// the whole delta log, and the log only grows. Both thresholds are ceilings on how
// much log a read can be made to replay, and a store with neither is unbounded by
// construction — it degrades from the first commit, with no threshold anyone
// crosses and no symptom until reads take seconds.
//
// The two are not equivalent, and MaxBytes is the one to rely on:
//
//   - MaxBytes measures the delta the log has accumulated since the last snapshot,
//     which is exactly what a read has to replay, and it is measured on the file —
//     so it means the same thing to a process that has just started as to one that
//     has been up for weeks.
//   - MaxCommits counts commits THIS process has seen. A server that restarts
//     before reaching the threshold starts counting again from zero, so a store
//     whose pod restarts often enough never snapshots however long it runs — which
//     is one of the two reasons a staging store reached 15 MB of log with an empty
//     snapshot file (issue ps8kfs9dh12kr777fnn0).
//
// Zero or negative disables a threshold. A config file with no snapshot section at
// all gets the defaults (see DefaultConfig); writing the section and leaving a
// threshold at zero is how it is turned off deliberately.
//
//tony:schemagen=snapshot-config
type SnapshotConfig struct {
	// MaxCommits triggers a snapshot after this many commits since the last snapshot
	// taken by this process. Zero or negative means disabled.
	MaxCommits int64 `tony:"field=maxCommits"`

	// MaxBytes triggers a snapshot once the active log has grown by this many bytes
	// since the last snapshot. Zero or negative means disabled.
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
	Cutoff Duration `tony:"field=cutoff"`

	// BaseInterval is the snapshot retention interval for the first tier after cutoff.
	// Default: 1h
	BaseInterval Duration `tony:"field=baseInterval"`

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
	GracePeriod Duration `tony:"field=gracePeriod"`
}

// ToStorageConfig converts to storage.CompactionConfig.
func (c *CompactionConfig) ToStorageConfig() *storage.CompactionConfig {
	if c == nil {
		return nil
	}
	cfg := storage.DefaultCompactionConfig()

	if c.Cutoff > 0 {
		cfg.Cutoff = time.Duration(c.Cutoff)
	}
	if c.BaseInterval > 0 {
		cfg.BaseInterval = time.Duration(c.BaseInterval)
	}
	if c.SlotsPerTier > 0 {
		cfg.SlotsPerTier = c.SlotsPerTier
	}
	if c.Multiplier > 0 {
		cfg.Multiplier = c.Multiplier
	}
	if c.GracePeriod > 0 {
		cfg.GracePeriod = time.Duration(c.GracePeriod)
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

	return cfg.WithDefaults(), nil
}

// Default snapshot thresholds. They are a guess, and a guess is the point: a store
// whose operator never thought about snapshotting still has to survive, and the
// alternative to a guess here is not a better number but unbounded growth.
//
// The byte threshold is the one that does the work — it is what read cost tracks,
// and it is measured on the file rather than counted in memory (see SnapshotConfig).
// 4 MiB is well under the 15 MB that took reads from milliseconds to seconds, and
// far above a single write, so it costs a store that is barely used nothing.
const (
	defaultSnapshotMaxCommits = 1000
	defaultSnapshotMaxBytes   = 4 << 20
	defaultTxTimeout          = 1 * time.Second
)

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return (&Config{}).WithDefaults()
}

// WithDefaults fills in the sections the config does not have and returns it. A
// section that IS present is left exactly as written, zeros included: writing
// `snapshot: {}` is how a threshold is turned off on purpose, and a default that
// overrode it would make that impossible to say.
//
// It is applied to loaded config files as well as to servers given no config at all,
// because the hole is the same either way — a file that configures a schema and says
// nothing about snapshots used to disable snapshotting silently.
func (c *Config) WithDefaults() *Config {
	if c.Snapshot == nil {
		c.Snapshot = &SnapshotConfig{
			MaxCommits: defaultSnapshotMaxCommits,
			MaxBytes:   defaultSnapshotMaxBytes,
		}
	}
	if c.Tx == nil {
		c.Tx = &TxConfig{Timeout: Duration(defaultTxTimeout)}
	}
	return c
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
