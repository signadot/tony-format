package storage

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// The schema, and changing it.
//
// A schema change is a COMMIT: one entry in the one log, numbered in the one sequence,
// taken under commitMu like every write, so the schema in force is a function of the
// commit -- every commit before it was lowered under the schema before, every commit
// after under this one -- and a read at a commit knows which schema it is looking
// through (SchemaAt). The entry carries no delta (dlog.NewSchemaEntry), and every root
// snapshot afterwards records the schema in force at its commit, which is what lets a
// compacted log, with the schema commit itself gone, still say which schema its
// surviving snapshots were taken under. The history is rebuilt from the log with the
// index, and persisted with it (index/schema_history.go).
//
// There is no pending schema. A migration used to be proposed, held pending beside a
// second index, and completed at a second snapshot; the second index was a copy of the
// first that lost every commit it missed, and the two phases bought nothing -- writes in
// between were lowered under the old schema regardless (090mbrhsh12ksfr8mhn0).

// SchemaRefusedError is a schema the store will not adopt, as the client's mistake: what
// it declares cannot mean what it says (api.Schema.Validate), or the data held cannot
// follow it (identityChangeAllowed). The remedy is the client's.
type SchemaRefusedError struct {
	Err error
}

func (e *SchemaRefusedError) Error() string { return "schema cannot be adopted: " + e.Err.Error() }
func (e *SchemaRefusedError) Unwrap() error { return e.Err }

// SetSchema makes schema the store's schema from the commit it answers on: one commit,
// after which every write is lowered under it. It is refused (SchemaRefusedError) when
// the schema cannot mean what it says, or when an array would gain or change an identity
// while it holds elements, which have no names under the new one. An array LOSING its
// identity is refused too, unless force: its elements stay held under their names, and
// read back as the object of names the store keeps rather than as an array.
//
// Setting the schema the store already has is a commit like any other setting: it takes
// a number and answers it.
func (s *Storage) SetSchema(schema *ir.Node, force bool) (int64, error) {
	// Under the commit lock with every write: the schema takes effect at a commit, and
	// a write is lowered under the schema of the commit it takes -- not one that changed
	// between its precondition and its append.
	s.commitMu.Lock()
	defer s.commitMu.Unlock()

	parsed := api.ParseSchemaFromNode(schema)
	if err := parsed.Validate(); err != nil {
		return 0, &SchemaRefusedError{Err: err}
	}
	if err := s.identityChangeAllowed(parsed, force); err != nil {
		return 0, &SchemaRefusedError{Err: err}
	}

	commit, err := s.sequence.NextCommit()
	if err != nil {
		return 0, fmt.Errorf("failed to allocate commit: %w", err)
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	entry := dlog.NewSchemaEntry(schema, commit, timestamp, commit-1)
	_, logFile, err := s.dLog.AppendEntry(entry)
	if err != nil {
		return 0, fmt.Errorf("failed to write the schema commit: %w", err)
	}
	if s.durability == DurabilitySync {
		if err := s.dLog.Sync(logFile); err != nil {
			return 0, fmt.Errorf("failed to sync log after the schema commit: %w", err)
		}
	}

	// Noted where the rebuild would note it, so the next persist carries it; and in
	// force from here on. Then readable: the watermark advances, with nothing for a
	// watcher to apply.
	s.index.NoteSchema(commit, schema)
	s.schema.append(commit, schema)
	s.tick.publish(commit, nil)

	s.logger.Info("schema set", "commit", commit, "keyedPaths", len(parsed.KeyedPaths()))
	return commit, nil
}

// BootstrapSchema is the configured schema meeting the store: a store with no schema of
// its own adopts it, as a commit; a store whose schema is the configured one has
// nothing to do; and a store whose schema DIFFERS is refused, naming both, since a
// configuration file is not where a schema changes -- a change is a commit, held to the
// checks SetSchema makes, and a file edited between restarts is held to none of them.
func (s *Storage) BootstrapSchema(schema *ir.Node) error {
	if schema == nil {
		return nil
	}
	active, at := s.schema.Active()
	if active == nil {
		commit, err := s.SetSchema(schema, false)
		if err != nil {
			return fmt.Errorf("adopting the configured schema: %w", err)
		}
		s.logger.Info("adopted the configured schema", "commit", commit)
		return nil
	}
	if SameSchema(active, schema) {
		return nil
	}
	return fmt.Errorf("the store's schema, set at commit %d, is not the configured one: "+
		"a schema changes by a schema request on a baseline session, not by editing the "+
		"configuration; configure the store's schema, or change it through the session",
		at)
}

// SameSchema reports whether two schema documents say the same thing.
func SameSchema(a, b *ir.Node) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return tony.Diff(a, b) == nil
}

// GetActiveSchema returns the current active schema and the commit where it was set.
// Returns nil schema and 0 commit if schemaless.
func (s *Storage) GetActiveSchema() (*ir.Node, int64) {
	return s.schema.Active()
}

// SchemaAt answers the schema in force at commit, and the commit that set it: the last
// one set at or before it, nil and 0 when none was.
func (s *Storage) SchemaAt(commit int64) (*ir.Node, int64) {
	return s.schema.At(commit)
}

// SchemaHistory answers every schema the store has had, in commit order.
func (s *Storage) SchemaHistory() []SchemaAt {
	var out []SchemaAt
	for _, sa := range s.index.SchemaHistory() {
		out = append(out, SchemaAt{Commit: sa.Commit, Schema: sa.Schema})
	}
	return out
}

// SchemaAt is one schema and the commit it took effect at.
type SchemaAt struct {
	Commit int64
	Schema *ir.Node
}

// identityChangeAllowed refuses a schema that changes which arrays have an identity in a
// way the stored data cannot follow.
//
// An array that GAINS an identity is the boundary between two regimes: before, it is one
// indexed path whose elements have no names; after, each element is a field. Elements
// already written by position have no names to be found under, so the identity is
// declared before the array is written, or the array is emptied first -- a refusal here,
// where it is a schema error, rather than elements that vanish from every read after
// the schema commit. Changing an identity strands the elements the same way. An array
// that LOSES one is refused for the same reason, unless forced: the elements stay under
// their names, and what was an array reads back as the object of names the store holds
// (element_identity.md). Deriving names for a gain, and an array back from names for a
// loss, is the rewrite 090mbrhsh12ksfr8mhn0 phase 2 adds.
//
// A SCOPE is a layer of statements replayed over baseline, and a scope's statement at a
// path is lowered under the identity the path had when it was made: an element by
// position, or under its name. The schema is per-store, so a change reaches every
// scope, and a scope's statement under a path whose identity changes would be replayed
// under the other regime -- positional elements read as an object of names, or names
// read as positions. The safe thing, until scopes and schema are worked through
// (090mbrhsh12ksfr8mhn0), is to refuse the change while any scope has a live statement
// at, above or beneath such a path, naming the scopes: the operator deletes or lands
// them first.
//
// Asked at the commit, under commitMu, so no write lands between the question and the
// schema taking effect.
func (s *Storage) identityChangeAllowed(pending *api.Schema, force bool) error {
	active := s.schema.ActiveParsed()
	commit := s.tick.current()
	scopesUnder := func(p string) []string {
		foot := s.index.Footprint()
		var out []string
		for _, id := range foot.Scopes() {
			if foot.Reaches(id, p) {
				out = append(out, id)
			}
		}
		return out
	}
	changes := func(p string) bool {
		return active.Keyed(p) != pending.Keyed(p) || !slices.Equal(active.Identity(p), pending.Identity(p))
	}
	for _, p := range slices.Concat(active.KeyedPaths(), pending.KeyedPaths()) {
		if !changes(p) {
			continue
		}
		if scopes := scopesUnder(p); len(scopes) > 0 {
			return fmt.Errorf("%q cannot change its identity while a scope has statements under it: %s",
				p, strings.Join(scopes, ", "))
		}
	}
	held := func(p string) (*ir.Node, error) {
		if commit == 0 {
			return nil, nil
		}
		c, err := s.Read(commit, nil, p)
		if err != nil {
			return nil, err
		}
		n, err := collectAll(c)
		if err != nil {
			return nil, err
		}
		return ir.Uncomment(n), nil
	}
	for _, p := range active.KeyedPaths() {
		if pending.Keyed(p) {
			continue
		}
		n, err := held(p)
		if err != nil {
			return err
		}
		if n == nil || n.Type != ir.ObjectType || len(n.Fields) == 0 {
			continue // nothing held under a name; nothing to strand
		}
		if force {
			s.logger.Warn("an array loses its identity by force; its elements stay under their names",
				"path", p, "identity", strings.Join(active.Identity(p), ","), "elements", len(n.Fields))
			continue
		}
		return fmt.Errorf("%q cannot lose its identity %s: its %d elements are held under their names "+
			"(force it, and they stay there, read back as an object of names)",
			p, strings.Join(active.Identity(p), ","), len(n.Fields))
	}
	for _, p := range pending.KeyedPaths() {
		n, err := held(p)
		if err != nil {
			return err
		}
		if active.Keyed(p) {
			if !slices.Equal(active.Identity(p), pending.Identity(p)) && n != nil && n.Type == ir.ObjectType && len(n.Fields) > 0 {
				return fmt.Errorf("%q cannot change its identity from %s to %s: its elements are held under their names",
					p, strings.Join(active.Identity(p), ","), strings.Join(pending.Identity(p), ","))
			}
			continue
		}
		if n != nil && n.Type == ir.ArrayType && len(n.Values) > 0 {
			return fmt.Errorf("%q cannot be given the identity %s: it already holds %d elements written by "+
				"position, which have no names; declare the identity before the array is written, or empty it first",
				p, strings.Join(pending.Identity(p), ","), len(n.Values))
		}
	}
	return nil
}

// IsSchemaRefused reports whether err is a schema the store refused to adopt.
func IsSchemaRefused(err error) bool {
	var r *SchemaRefusedError
	return errors.As(err, &r)
}
