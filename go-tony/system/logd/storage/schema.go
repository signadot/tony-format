package storage

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/libdiff"
	tschema "github.com/signadot/tony-format/go-tony/schema"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
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
// A change to an identity is carried by the commit as a rewrite of the arrays it
// changes (identityRewrite), so the data and the schema move together, at one commit.
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
// after which every write is lowered under it. Where the schema changes an array's
// identity, the commit carries the REWRITE: the array restated in the new schema's form,
// as a total cover at its path (identityRewrite). It is refused (SchemaRefusedError)
// when the schema cannot mean what it says, when an element cannot be named under the
// new identity -- a key missing, or two elements with one name -- when a scope has
// statements under a path whose identity changes, or when an array would lose its
// identity and force is not given.
//
// Setting the schema the store already has is a commit like any other setting: it takes
// a number and answers it.
func (s *Storage) SetSchema(schema *ir.Node, force bool) (int64, error) {
	// Under the commit lock with every write: the schema takes effect at a commit, and
	// a write is lowered under the schema of the commit it takes -- not one that changed
	// between its precondition and its append.
	s.commitMu.Lock()
	defer s.commitMu.Unlock()

	// A schema document first -- an object, with define an object, and every definition
	// one the schema package can build -- and then what logd reads out of it.
	if _, err := tschema.ParseSchema(schema); err != nil {
		return 0, &SchemaRefusedError{Err: err}
	}
	parsed := api.ParseSchemaFromNode(schema)
	if err := parsed.Validate(); err != nil {
		return 0, &SchemaRefusedError{Err: err}
	}
	if err := s.identityChangeAllowed(parsed, force); err != nil {
		return 0, &SchemaRefusedError{Err: err}
	}

	// The number first: a generated id is minted from the commit it lands in. A refusal
	// past here leaves a gap in the sequence, as a write refused at its append does.
	commit, err := s.sequence.NextCommit()
	if err != nil {
		return 0, fmt.Errorf("failed to allocate commit: %w", err)
	}
	rewrite, err := s.identityRewrite(parsed, commit)
	if err != nil {
		var wb *WriteBudgetError
		if errors.As(err, &wb) {
			return 0, err
		}
		return 0, &SchemaRefusedError{Err: err}
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	entry := dlog.NewSchemaEntry(schema, rewrite, commit, timestamp, commit-1)
	pos, logFile, err := s.dLog.AppendEntry(entry)
	if err != nil {
		return 0, fmt.Errorf("failed to write the schema commit: %w", err)
	}
	if s.durability == DurabilitySync {
		if err := s.dLog.Sync(logFile); err != nil {
			return 0, fmt.Errorf("failed to sync log after the schema commit: %w", err)
		}
	}

	// Noted where the rebuild would note it, so the next persist carries it; and in
	// force from here on. The rewrite is indexed and published as any delta is, raised
	// into the client's vocabulary under the schema it was written in -- the new one.
	s.index.NoteSchema(commit, schema)
	s.schema.append(commit, schema)
	if rewrite != nil {
		generation := s.dLog.GetGeneration(logFile)
		index.IndexPatch(s.index, entry, string(logFile), pos, 0, generation, rewrite, nil)
		if s.indexPersister != nil {
			s.indexPersister.MaybePersist(commit)
		}
		n := newCommitNotification(commit, 0, timestamp, rewrite, nil)
		n.Patch = s.raiseDelta(nil, n.Patch, commit)
		s.tick.publish(commit, n)
	} else {
		s.tick.publish(commit, nil)
	}

	s.logger.Info("schema set", "commit", commit, "keyedPaths", len(parsed.KeyedPaths()), "rewrite", rewrite != nil)
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

// identityChangeAllowed refuses a schema that changes an identity where the change cannot
// be made.
//
// An array that LOSES its identity is refused unless forced: the change is a rewrite that
// answers -- the elements come back as an array, in name order -- but it is the one
// change that loses information, the names, so it is asked for twice.
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
	foot := s.index.Footprint()
	for _, p := range identityChanges(active, pending) {
		var scopes []string
		for _, id := range foot.Scopes() {
			if foot.Reaches(id, p) {
				scopes = append(scopes, id)
			}
		}
		if len(scopes) > 0 {
			return fmt.Errorf("%q cannot change its identity while a scope has statements under it: %s",
				p, strings.Join(scopes, ", "))
		}
		if active.Keyed(p) && !pending.Keyed(p) && !force {
			return fmt.Errorf("%q would lose its identity %s, and its elements their names: force it, "+
				"and they come back as an array in name order", p, strings.Join(active.Identity(p), ","))
		}
	}
	return nil
}

// identityChanges answers the paths whose identity differs between the two schemas, in
// order, outermost first: a keyed array beneath another that changes is restated with
// its container, and is not named on its own.
func identityChanges(active, pending *api.Schema) []string {
	var out []string
	for _, p := range slices.Concat(active.KeyedPaths(), pending.KeyedPaths()) {
		if slices.Contains(out, p) {
			continue
		}
		if active.Keyed(p) != pending.Keyed(p) || !slices.Equal(active.Identity(p), pending.Identity(p)) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	var top []string
	for _, p := range out {
		under := false
		for _, q := range top {
			if isKPathPrefix(q, p) {
				under = true
				break
			}
		}
		if !under {
			top = append(top, p)
		}
	}
	return top
}

func isKPathPrefix(q, p string) bool {
	qs, ps := kpath.SplitAll(q), kpath.SplitAll(p)
	if len(qs) >= len(ps) {
		return false
	}
	return slices.Equal(qs, ps[:len(qs)])
}

// identityRewrite answers the delta a schema change makes to the data: every array
// whose identity changes, restated in the new schema's form as a total cover at its
// path, rooted together into one patch -- or nil when no array holds anything to
// restate.
//
// An array GAINS an identity: its elements, written by position, are named from their
// key fields -- an auto-id is generated for an element that lacks one, minted from
// commit as a write's would be -- and held under the names (tx.LowerKeyed, which
// refuses an element with no name and two elements with one). It LOSES one: the
// elements come back as an array, in name order, which is the order a read answered
// them in. It CHANGES: the elements are named again, under the new key fields. Each
// array is read whole, under the write budget: the rewrite is a write of the array,
// and is bounded as one.
func (s *Storage) identityRewrite(pending *api.Schema, commit int64) (*ir.Node, error) {
	active := s.schema.ActiveParsed()
	head := s.tick.current()
	if head == 0 {
		return nil, nil
	}
	var pds []*tx.PatcherData
	for _, p := range identityChanges(active, pending) {
		base, err := s.stateAt(head, nil, p)
		if err != nil {
			return nil, err
		}
		base = ir.Uncomment(base)
		if base == nil {
			continue
		}
		// In the client's form under the schema that wrote it: an array either way.
		raised := base
		if active.Keyed(p) {
			raised = raiseKeyed(active, base, p, false, false)
		}
		if raised.Type != ir.ArrayType || len(raised.Values) == 0 {
			continue // nothing held, nothing to restate
		}
		next := raised.Clone()
		if pending.Keyed(p) {
			one := []*tx.PatcherData{{API: &api.Patch{PathData: api.PathData{Path: p, Data: next}}}}
			tx.InjectAutoIDs(commit, pending, one)
			if err := tx.LowerKeyed(pending, one); err != nil {
				return nil, err
			}
			next = one[0].API.Data
		}
		// A total cover: !insert answers with its child whatever was there, and what was
		// there is the other form (mergeop's insert; claimValue says the same).
		next = next.WithTag(libdiff.InsertTag)
		pds = append(pds, &tx.PatcherData{API: &api.Patch{PathData: api.PathData{Path: p, Data: next}}})
	}
	if len(pds) == 0 {
		return nil, nil
	}
	out, err := tx.MergePatches(pds)
	if err != nil {
		return nil, fmt.Errorf("rooting the rewrite: %w", err)
	}
	if err := api.ValidateForStorage(out); err != nil {
		return nil, fmt.Errorf("the rewrite is not storable: %w", err)
	}
	return out, nil
}

// IsSchemaRefused reports whether err is a schema the store refused to adopt.
func IsSchemaRefused(err error) bool {
	var r *SchemaRefusedError
	return errors.As(err, &r)
}
