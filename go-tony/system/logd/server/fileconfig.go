package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/schema"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// Config represents the logd server configuration file structure.
// Designed for extensibility - new sections can be added without breaking existing configs.
//
//tony:schemagen=config
type Config struct {
	// Schema is the Tony schema node that defines data model constraints.
	// Use !tovalue.file to load from a file: schema: !tovalue.file path/to/schema.tony
	// The schema is used to identify keyed arrays: auto-id fields (tagged with
	// !logd-auto-id) and client-supplied keys (tagged with !logd-key). A schema the
	// store holds from a migration takes precedence over it. If both are nil, no
	// array is keyed and no id is generated.
	Schema *ir.Node `tony:"field=schema"`

	// Snapshot configures automatic snapshotting behavior.
	Snapshot *SnapshotConfig `tony:"field=snapshot"`

	// Tx configures transaction behavior.
	Tx *TxConfig `tony:"field=tx"`

	// Compaction configures logarithmic retention policy for the inactive log.
	// If nil, compaction is disabled (all data retained).
	Compaction *CompactionConfig `tony:"field=compaction"`

	// Retention configures the rules that age log-like records out of the STATE.
	// If nil, nothing is ever deleted by the server. See RetentionConfig.
	Retention *RetentionConfig `tony:"field=retention"`

	// Storage configures the storage layer itself.
	// If nil, storage defaults apply.
	Storage *StorageConfig `tony:"field=storage"`
}

// RetentionConfig is the policy that ages log-like records out of the state.
//
// It is not compaction. Compaction removes the MEMORY of how the state was reached and
// never the state (docs/logd/compaction.md); a record that grows a log-like collection
// without bound is state, and only a write removes it. So retention is a writer: on a
// timer it reads each rule's container, finds the items whose own timestamp is older
// than the rule allows, and commits an ordinary delete for them -- under a system
// author, through the schema, with a precondition on what it read, in batches of a
// bounded size. Watchers see the deletes as authored deltas; a read at an older commit
// still shows the records; and the delete leaves the disk when compaction's horizon
// passes it (CompactionConfig.Horizon).
//
// Items are assumed to be log-like, which is why a rule REQUIRES the timestamp field:
// age read from the record is exact, depends on no compaction setting, and needs no
// new metadata in the store (issue regx2d1mh12krm0amnn0).
//
//tony:schemagen=retention-config
type RetentionConfig struct {
	// Every is how often the rules run. Zero is the default, 1h. A timer rather than
	// the snapshot trigger: a quiet log never snapshots, so its records would never
	// expire.
	Every Duration `tony:"field=every"`

	// Batch is the most items one delete commit removes; a rule with more expired
	// items than this takes several commits in one run. Zero is the default, 256.
	Batch int `tony:"field=batch"`

	// Author is what the delete commits are recorded as written by. Zero is the
	// default, "logd/retention".
	Author string `tony:"field=author"`

	// Rules are the rules, each over one container of items.
	Rules []*RetentionRule `tony:"field=rules"`
}

// RetentionRule ages the items of one container out of the state.
//
//tony:schemagen=retention-rule
type RetentionRule struct {
	// Path names the items: a path whose LAST segment is the wildcard of the
	// container's kind -- `jobs.*` for the fields of an object, `events{*}` for the
	// entries of a sparse array, `runs(*)` for the elements of a keyed array -- and
	// whose other segments are concrete. Each item is deleted whole, never a field
	// inside it.
	//
	// A dense array, `[*]`, is refused: an index names a position, not an element,
	// so a concurrent insert or delete lands the expiry on a neighbour and every
	// expiry shifts every later positional watch. Declare the array keyed instead
	// (!logd-key or !logd-auto-id in the schema) and write the rule with `(*)`.
	// `..` is refused as it is everywhere a path must name a place.
	Path string `tony:"field=path"`

	// Match, if set, is a pattern the item must match to expire, in the match
	// language (docs/matchpatch.md): `{status: !or [done, canceled]}`. Match tests
	// structure and equality and has no ordering, which is why age is not a match.
	Match *ir.Node `tony:"field=match"`

	// Age is the path, inside the item, of the RFC3339 timestamp its age is read
	// from: `.updatedAt`, `meta.at`. An item with no parseable timestamp there is
	// kept, and reported once per run.
	Age string `tony:"field=age"`

	// After is how old an item may be before it expires: it expires when
	// now - timestamp >= after, and Match (if any) holds.
	After Duration `tony:"field=after"`
}

// Retention defaults. An hour is the granularity anyone asks for a retention rule
// in; a batch of 256 keeps one commit's delta, and the watch event it becomes,
// small whatever the backlog.
const (
	defaultRetentionEvery  = time.Hour
	defaultRetentionBatch  = 256
	defaultRetentionAuthor = "logd/retention"
)

// WithDefaults fills the zero fields of a retention section and returns it. A section
// that is present with no rules configures nothing, and says so at load (Validate).
func (c *RetentionConfig) WithDefaults() *RetentionConfig {
	if c == nil {
		return nil
	}
	if c.Every == 0 {
		c.Every = Duration(defaultRetentionEvery)
	}
	if c.Batch == 0 {
		c.Batch = defaultRetentionBatch
	}
	if c.Author == "" {
		c.Author = defaultRetentionAuthor
	}
	return c
}

// Validate refuses a retention section that cannot mean what it says, at load: a
// negative interval or batch, and any rule Validate refuses.
func (c *RetentionConfig) Validate() error {
	if c == nil {
		return nil
	}
	if c.Every < 0 {
		return fmt.Errorf("retention: every %v is negative", time.Duration(c.Every))
	}
	if c.Batch < 0 {
		return fmt.Errorf("retention: batch %d is negative", c.Batch)
	}
	if len(c.Rules) == 0 {
		return errors.New("retention: a section with no rules retains nothing; leave it out, or give it rules")
	}
	for i, r := range c.Rules {
		if r == nil {
			return fmt.Errorf("retention: rule %d is empty", i)
		}
		if _, err := r.target(); err != nil {
			return fmt.Errorf("retention: rule %d: %w", i, err)
		}
	}
	return nil
}

// retentionTarget is a rule, read: the container its items are the children of, the
// kind its items are named by, and the path of an item's timestamp.
type retentionTarget struct {
	container string
	items     kpath.EntryKind
	age       *kpath.KPath
}

// target reads a rule, and is where a rule is refused: a path that names one node,
// or a set of them at more than one step, or the positions of a dense array; an age
// that is not a field path; a duration that is not one.
func (r *RetentionRule) target() (*retentionTarget, error) {
	kp, err := kpath.Parse(r.Path)
	if err != nil {
		return nil, fmt.Errorf("path %q: %w", r.Path, err)
	}
	if kp == nil {
		return nil, errors.New("path is empty: a rule names a container's items, as jobs.*")
	}
	var last *kpath.KPath
	for x := kp; x != nil; x = x.Next {
		if x.Descend {
			return nil, fmt.Errorf("path %q: `..` names nodes at any depth, and a rule's items are the children of one container", r.Path)
		}
		if x.Next == nil {
			last = x
			break
		}
		if x.Wild() {
			return nil, fmt.Errorf("path %q: only the last segment may be a wildcard; a rule's items are the children of one container", r.Path)
		}
	}
	if !last.Wild() {
		return nil, fmt.Errorf("path %q names one node; a rule's last segment names its items: .* for an object's fields, {*} for a sparse array's entries, (*) for a keyed array's elements", r.Path)
	}
	if last.IndexAll {
		return nil, fmt.Errorf("path %q: [*] names positions, and a position is not an element -- a concurrent write lands the expiry on a neighbour; declare the array keyed (!logd-key or !logd-auto-id) and write the rule with (*)", r.Path)
	}
	t := &retentionTarget{container: kp.Parent().String(), items: last.EntryKind()}
	if kp.Parent() == nil {
		t.container = ""
	}

	age, err := kpath.Parse(r.Age)
	if err != nil {
		return nil, fmt.Errorf("age %q: %w", r.Age, err)
	}
	if age == nil {
		return nil, errors.New("age is empty: a rule names the field an item's timestamp is in, as .updatedAt")
	}
	for x := age; x != nil; x = x.Next {
		if x.Field == nil {
			return nil, fmt.Errorf("age %q: a timestamp is at a field path inside the item, as .updatedAt or meta.at", r.Age)
		}
	}
	t.age = age

	// A match is combined with the timestamp into the delete's precondition, one
	// object pattern, so it has to be one: `{status: !or [done, canceled]}` rather than
	// `!or [...]` at the item. And it must not name the age field itself: age is the
	// rule's comparison, and a match on that field would be a second, contradictory,
	// answer to when an item expires.
	if r.Match != nil {
		m := ir.Uncomment(r.Match)
		if m == nil || m.Type != ir.ObjectType {
			return nil, errors.New("match is not an object pattern; write it as {field: pattern, ...}, as {status: !or [done, canceled]}")
		}
		if ir.Get(m, *age.Field) != nil {
			return nil, fmt.Errorf("match names %q, which is the age field: age is compared by `after`, not by the match", *age.Field)
		}
	}

	if r.After <= 0 {
		return nil, fmt.Errorf("after %v: an item expires after a positive duration", time.Duration(r.After))
	}
	return t, nil
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

	// ReadBudget is the largest node the server builds to answer one read, in bytes:
	// a match, a watch's initial state, a watch's read of the value at its path. A read
	// past it is refused rather than held. Zero means the default, 64 MiB.
	ReadBudget int64 `tony:"field=readBudget"`

	// WriteBudget is the largest node the store builds to verify or lower one write, or
	// to evaluate one precondition, in bytes: the value at a path the write names. A
	// write whose verification needs more is refused, naming the path and the size.
	// Zero means the default, 128 MiB.
	WriteBudget int64 `tony:"field=writeBudget"`

	// PathSnapshotTail is how many records a read may fold at a path before it takes
	// a snapshot there, for each PathSnapshotBytes of the subtree: a subtree N times
	// PathSnapshotBytes must fold more than N times the tail. PathSnapshotBytes is a
	// rate, not a ceiling. Zero means the defaults (64 records, 1 MiB); a negative tail
	// turns per-path snapshots off. See storage/path_snapshot.go.
	PathSnapshotTail  int64 `tony:"field=pathSnapshotTail"`
	PathSnapshotBytes int64 `tony:"field=pathSnapshotBytes"`

	// IndexCeiling bounds what the index holds resident, in bytes; past it the least
	// recently used regions are evicted to the durable index and paged back on a miss.
	// Zero is unbounded. See storage/index/region.go.
	IndexCeiling int64 `tony:"field=indexCeiling"`
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

// Duration is a length of time written the way a person writes one: "1h", "30s",
// "500ms" — what time.ParseDuration reads and what time.Duration prints — and, since
// retention is asked for in days and years rather than hours, "1d", "2w" and "1y" too,
// as 24h, 7d and 365d. They mix with the rest: "1y6w", "1d12h".
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

// UnmarshalText reads what time.ParseDuration reads, plus the d, w and y units.
func (d *Duration) UnmarshalText(text []byte) error {
	v, err := ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// ParseDuration is time.ParseDuration with days, weeks and years: a "d" is 24h, a "w"
// 7d and a "y" 365d, calendar-blind, which is what a retention limit means by them.
func ParseDuration(s string) (time.Duration, error) {
	var out strings.Builder
	rest := s
	for rest != "" {
		i := 0
		for i < len(rest) && (rest[i] >= '0' && rest[i] <= '9' || rest[i] == '.') {
			i++
		}
		j := i
		for j < len(rest) && !(rest[j] >= '0' && rest[j] <= '9' || rest[j] == '.') {
			j++
		}
		num, unit := rest[:i], rest[i:j]
		rest = rest[j:]
		var hours float64
		switch unit {
		case "d":
			hours = 24
		case "w":
			hours = 24 * 7
		case "y":
			hours = 24 * 365
		default:
			out.WriteString(num + unit)
			continue
		}
		n, err := strconv.ParseFloat(num, 64)
		if err != nil || num == "" {
			return 0, fmt.Errorf("time: invalid duration %q", s)
		}
		fmt.Fprintf(&out, "%gh", n*hours)
	}
	return time.ParseDuration(out.String())
}

// TxConfig configures transaction behavior.
//
//tony:schemagen=tx-config
type TxConfig struct {
	// Timeout is the maximum time to wait for all participants to join a transaction:
	// what a newtx naming no timeout gets, and the most a newtx may ask for -- one
	// asking more is refused (invalid_tx). If not all participants join within this
	// duration, the transaction is aborted and waiting participants receive a timeout
	// error.
	// Default: 5m. Zero is the default too: there is no transaction without a timeout
	// (hqhyyat8h12ksarmcdn0).
	Timeout Duration `tony:"field=timeout"`
}

// SnapshotConfig configures when automatic snapshots are triggered.
//
// A snapshot is what bounds the cost of reading: without one, a read replays every
// write that reaches its path since the log began, and the log only grows. Both
// thresholds are ceilings on how much log a read can be made to replay, and a store
// with neither is unbounded by construction — it degrades from the first commit, with
// no threshold anyone crosses and no symptom until reads take seconds.
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

	// Horizon is how far back any history survives at all: past it a snapshot is
	// dropped whatever tier it would have taken a slot in. It is what makes a record
	// that retention deleted leave the disk, rather than survive in ever-sparser old
	// snapshots. It must be at least the cutoff. Default: none (zero).
	Horizon Duration `tony:"field=horizon"`
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
	if c.Horizon > 0 {
		cfg.Horizon = time.Duration(c.Horizon)
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
	// A file with no DOCUMENT in it -- empty, or nothing but comments and blank lines --
	// configures nothing, which is what an operator who has commented every setting out
	// means by it. It is the same answer as passing no config file at all: the defaults.
	// It is read as an empty document rather than returned early so that it takes the one
	// path every other config takes, defaults and validation included.
	//
	// Parsing such a file answers a nil node, and everything downstream of here assumes a
	// document: expansion clones it, which dereferenced the nil and took `o sys up` down
	// with a segfault rather than a message an operator could act on.
	if node == nil {
		node = ir.FromMap(map[string]*ir.Node{})
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
	defaultTxTimeout          = tx.DefaultTimeout
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
	c.Retention = c.Retention.WithDefaults()
	return c
}

// Validate checks the configuration for errors. Called by LoadConfig, so a file
// giving a value logd does not understand -- a durability other than os or sync, a
// schema api.Schema.Validate refuses, a compaction policy the store would refuse, or a
// retention rule that names no items -- is rejected rather than run.
func (c *Config) Validate() error {
	// A misspelled durability must not fall back to the default: an operator who
	// wrote "fsync" and silently got page-cache writes has the opposite of what
	// they asked for, and would not find out until a crash.
	if _, err := c.Storage.ToStorageDurability(); err != nil {
		return err
	}
	// The policy the store will run, checked where the operator is: it used to be
	// checked inside every compaction instead, so `multiplier: 1` loaded without
	// complaint and then failed, best-effort and logged, on every snapshot -- a store
	// that quietly never compacted.
	if c.Compaction != nil {
		if err := c.Compaction.ToStorageConfig().Validate(); err != nil {
			return err
		}
	}
	if err := c.Retention.Validate(); err != nil {
		return err
	}
	// A schema is held to the rules a migration is held to. The config's was adopted
	// without them, so a store ran a schema its own SetSchema refuses -- an array
	// declared both keyed and auto-id, two identities for one array (khkedy9wh12ksyxxmdn0).
	if c.Schema != nil {
		if _, err := schema.ParseSchema(c.Schema); err != nil {
			return fmt.Errorf("schema: %w", err)
		}
		if err := api.ParseSchemaFromNode(c.Schema).Validate(); err != nil {
			return fmt.Errorf("schema: %w", err)
		}
	}
	return nil
}
