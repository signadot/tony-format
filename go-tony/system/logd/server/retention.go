package server

import (
	"errors"
	"fmt"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/ident"
)

// Retention: the writer that ages log-like records out of the state (RetentionConfig).
//
// It is a client of the store like any other, in the server's process: it reads a
// rule's container, decides per item from the item's own timestamp and the rule's
// match, and commits a delete for the expired ones through the same transaction path a
// session's patch takes -- lowered under the schema, checked against it, published to
// watchers under its author. Nothing in storage knows it exists.
//
// A pass reads the container at one commit and the delete carries, as its precondition,
// what it read of each item it deletes: the timestamp, and the match's fields. A writer
// that touches one of those items between the read and the commit fails that batch's
// precondition, the batch is skipped, and the next pass reads again. That is the
// compare-and-swap the patch protocol already has, used for what it is for.

// startRetention starts the retention timer, if the configuration has rules. The first
// pass runs at once, so a backlog is not left waiting an interval to be noticed.
func (s *Server) startRetention() {
	cfg := s.Spec.Config.Retention
	if cfg == nil || len(cfg.Rules) == 0 || s.Spec.Storage == nil || s.retentionStop != nil {
		return
	}
	s.retentionStop = make(chan struct{})
	s.retentionDone = make(chan struct{})
	every := time.Duration(cfg.Every)
	go func() {
		defer close(s.retentionDone)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			s.retentionPass(time.Now())
			select {
			case <-s.retentionStop:
				return
			case <-ticker.C:
			}
		}
	}()
}

// stopRetention stops the timer and waits for a pass in progress: the store is closed
// after the server stops, and a pass must not be committing into it then.
func (s *Server) stopRetention() {
	if s.retentionStop == nil {
		return
	}
	close(s.retentionStop)
	<-s.retentionDone
	s.retentionStop, s.retentionDone = nil, nil
}

// retentionPass runs every rule once, as of now, and answers how many items were
// deleted. A rule that fails is logged and the others still run; the error answered
// is every rule's, joined, for a caller that wants to know.
func (s *Server) retentionPass(now time.Time) (deleted int, err error) {
	cfg := s.Spec.Config.Retention
	if cfg == nil {
		return 0, nil
	}
	var errs []error
	for i, rule := range cfg.Rules {
		n, err := s.applyRetentionRule(now, cfg, rule)
		deleted += n
		if err != nil {
			s.Spec.Log.Error("retention rule failed", "rule", i, "path", rule.Path, "error", err)
			errs = append(errs, fmt.Errorf("rule %d (%s): %w", i, rule.Path, err))
		}
	}
	return deleted, errors.Join(errs...)
}

// applyRetentionRule runs one rule: reads its container as of the current commit,
// selects the expired items, and deletes them in batches.
func (s *Server) applyRetentionRule(now time.Time, cfg *RetentionConfig, rule *RetentionRule) (int, error) {
	t, err := rule.target()
	if err != nil {
		return 0, err
	}
	store := s.Spec.Storage
	commit, err := store.GetCurrentCommit()
	if err != nil {
		return 0, err
	}
	if commit == 0 {
		return 0, nil
	}
	schema := store.SchemaFor(nil)
	container, err := ident.CanonicalPath(schema, t.container)
	if err != nil {
		return 0, err
	}
	// The rule says what kind of thing its items are, and the schema is the authority on
	// whether the container has an identity. The two have to agree, so a rule written
	// `.*` over an array that is keyed, or `(*)` over one that is not, is a rule about a
	// container that does not exist.
	keyed := schema.Keyed(schemaPath(container))
	switch {
	case t.items == kpath.KeyEntry && !keyed:
		return 0, fmt.Errorf("%s(*): the schema gives %s no identity; its items are not keyed elements", t.container, placeOrRoot(t.container))
	case t.items != kpath.KeyEntry && keyed:
		return 0, fmt.Errorf("%s is keyed, so its items are elements; write the rule as %s(*)", placeOrRoot(t.container), t.container)
	}

	c, err := store.Read(commit, nil, container)
	if err != nil {
		return 0, err
	}
	if c.Presence() == storage.Absent {
		c.Close()
		return 0, nil
	}
	node, err := storage.Collect(c, s.readBudget())
	if err != nil {
		return 0, err
	}
	node = ir.Uncomment(node)
	if node == nil {
		return 0, nil
	}
	switch node.Type {
	case ir.ObjectType:
	case ir.ArrayType:
		// The store keeps a keyed array as an object of names, so an array here is a
		// dense one, whatever the rule says its items are.
		return 0, fmt.Errorf("%s is a dense array, and a position is not an element: declare it keyed (!logd-key or !logd-auto-id) and write the rule with (*)", placeOrRoot(t.container))
	default:
		return 0, fmt.Errorf("%s is a %s, which has no items", placeOrRoot(t.container), node.Type)
	}

	// Select. An item with no timestamp the rule can read is kept, and said once.
	var expired []retentionItem
	unreadable := 0
	for i, key := range node.Fields {
		if i >= len(node.Values) {
			break
		}
		item := ir.Uncomment(node.Values[i])
		ts, ok := timestampAt(item, t.age)
		if !ok {
			unreadable++
			continue
		}
		if now.Sub(ts) < time.Duration(rule.After) {
			continue
		}
		if rule.Match != nil {
			matched, err := tony.Match(item, rule.Match)
			if err != nil {
				return 0, fmt.Errorf("match against %s: %w", key.String, err)
			}
			if !matched {
				continue
			}
		}
		expired = append(expired, retentionItem{key: key, item: item})
	}
	if unreadable > 0 {
		s.Spec.Log.Warn("retention: items with no timestamp were kept",
			"path", rule.Path, "age", rule.Age, "items", unreadable)
	}

	// Delete, in batches.
	deleted := 0
	for len(expired) > 0 {
		n := min(cfg.Batch, len(expired))
		batch := expired[:n]
		expired = expired[n:]
		committed, err := s.commitRetentionBatch(container, node.Tag, batch, t, rule, cfg.Author)
		if err != nil {
			return deleted, err
		}
		deleted += committed
	}
	if deleted > 0 {
		s.Spec.Log.Info("retention: deleted expired items", "path", rule.Path, "items", deleted, "after", time.Duration(rule.After))
	}
	return deleted, nil
}

// retentionItem is one expired item: its name in the container, and what was read.
type retentionItem struct {
	key  *ir.Node
	item *ir.Node
}

// commitRetentionBatch deletes one batch of items from the container in one commit,
// under the precondition that each is as it was read. A precondition that no longer
// holds skips the batch -- the state moved under the pass, and the next pass reads it
// again -- and answers 0, not an error.
func (s *Server) commitRetentionBatch(container, containerTag string, batch []retentionItem, t *retentionTarget, rule *RetentionRule, author string) (int, error) {
	patch := &ir.Node{Type: ir.ObjectType}
	match := &ir.Node{Type: ir.ObjectType}
	// A sparse array is an object whose keys are numbers, and the tag is what says so:
	// a patch without it is an object, and merging an object over a sparse array
	// replaces it -- every entry gone, not the expired ones.
	if ir.TagHas(containerTag, ir.IntKeysTag) {
		patch.Tag = ir.TagCompose(ir.IntKeysTag, nil, "")
		match.Tag = patch.Tag
	}
	for _, it := range batch {
		addField(patch, it.key, &ir.Node{Type: ir.NullType, Tag: "!delete"})
		addField(match, it.key, retentionPrecondition(it.item, t.age, rule.Match))
	}
	store := s.Spec.Storage
	txn, err := store.NewTxWithTimeout(1, nil, 0, author)
	if err != nil {
		return 0, err
	}
	p, err := txn.NewPatcher(&api.Patch{
		Match:    &api.PathData{Path: container, Data: match},
		PathData: api.PathData{Path: container, Data: patch},
	})
	if err != nil {
		return 0, err
	}
	res := p.Commit()
	if res.Error != nil {
		return 0, res.Error
	}
	if !res.Matched {
		s.Spec.Log.Info("retention: batch skipped, the state moved under it", "path", rule.Path, "items", len(batch))
		return 0, nil
	}
	s.onCommit()
	return len(batch), nil
}

// retentionPrecondition is what the delete of an item asserts about it: the timestamp
// the pass read, at its path, and the fields the rule's match names. Both are combined
// into one object pattern, which is why a match has to be one (RetentionRule.target).
func retentionPrecondition(item *ir.Node, age *kpath.KPath, match *ir.Node) *ir.Node {
	var cond *ir.Node
	if m := ir.Uncomment(match); m != nil {
		cond = m.Clone()
	} else {
		cond = &ir.Node{Type: ir.ObjectType}
	}
	at := cond
	for x := age; x != nil; x = x.Next {
		if x.Next == nil {
			ts, _ := valueAt(item, age)
			addField(at, ir.FromString(*x.Field), ts.Clone())
			break
		}
		next := ir.Get(at, *x.Field)
		if next == nil {
			next = &ir.Node{Type: ir.ObjectType}
			addField(at, ir.FromString(*x.Field), next)
		}
		at = next
	}
	return cond
}

// timestampAt reads the item's timestamp at the age path: an RFC3339 string, or nothing.
func timestampAt(item *ir.Node, age *kpath.KPath) (time.Time, bool) {
	v, ok := valueAt(item, age)
	if !ok || v.Type != ir.StringType {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, v.String)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

// valueAt walks a field path inside an item.
func valueAt(item *ir.Node, path *kpath.KPath) (*ir.Node, bool) {
	at := ir.Uncomment(item)
	for x := path; x != nil; x = x.Next {
		if at == nil || at.Type != ir.ObjectType {
			return nil, false
		}
		at = ir.Uncomment(ir.Get(at, *x.Field))
	}
	if at == nil {
		return nil, false
	}
	return at, true
}

// addField appends a field to an object, keyed by a clone of key -- a string for an
// object's field, a number for a sparse array's entry -- wiring the parents the way
// ir.FromMap does.
func addField(obj, key, value *ir.Node) {
	i := len(obj.Fields)
	k := key.Clone()
	k.Parent, k.ParentIndex, k.ParentField = obj, i, key.String
	value.Parent, value.ParentIndex, value.ParentField = obj, i, key.String
	obj.Fields = append(obj.Fields, k)
	obj.Values = append(obj.Values, value)
}

// schemaPath is the schema's path for a document path: the same steps, with the names
// of keyed elements elided, since elements inherit their array's path (tx.LowerKeyed).
func schemaPath(path string) string {
	out := ""
	for _, seg := range kpath.SplitAll(path) {
		if name, isField := kpath.SegmentFieldName(seg); isField {
			if _, isName, err := ident.Parse(name); err == nil && isName {
				continue
			}
			out = kpath.ChildField(out, name)
			continue
		}
		out = kpath.Join(out, seg)
	}
	return out
}

func placeOrRoot(path string) string {
	if path == "" {
		return "the root"
	}
	return path
}

// readBudget is the largest node a retention pass builds to read one container: the
// configured read budget, which is what a session's read is held to.
func (s *Server) readBudget() int64 {
	if st := s.Spec.Config.Storage; st != nil && st.ReadBudget > 0 {
		return st.ReadBudget
	}
	return DefaultReadBudget
}
