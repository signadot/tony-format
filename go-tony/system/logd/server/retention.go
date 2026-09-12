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

// Retention: the request that ages log-like records out of the state (api.RetainRequest).
//
// Compaction removes the memory of how the state was reached and never the state; a
// record nobody deletes is state, and only a write removes it. A retain request is that
// write. It reads each rule's container in the session's view, decides per item from
// the item's own timestamp and the rule's match, and commits a delete for the expired
// ones through the transaction path a patch takes -- lowered under the schema, checked
// against it, published to watchers under its author. Nothing in storage knows it
// exists, and logd holds no policy and no clock for it: the caller carries the rules,
// the time and the author, and the commit is the record that it ran.
//
// A pass reads a container at one commit and each batch's delete carries, as its
// precondition, what it read of each item: the timestamp, and the match's fields. A
// writer that touches one of those items between the read and the commit fails that
// batch's precondition, the batch is skipped and said so in the result, and the caller
// asks again. That is the compare-and-swap the patch protocol already has.

// handleRetain runs a retain request on the loop, as a plain patch runs.
func (s *Session) handleRetain(id *string, req *api.RetainRequest) {
	now := time.Now()
	if req.Now != "" {
		t, err := time.Parse(time.RFC3339, req.Now)
		if err != nil {
			s.sendError(id, api.ErrCodeInvalidRetain, fmt.Sprintf("now %q is not an RFC3339 time: %v", req.Now, err))
			return
		}
		now = t
	}
	if len(req.What) == 0 {
		s.sendError(id, api.ErrCodeInvalidRetain, "what is empty: a retain request names at least one rule")
		return
	}
	batch := req.Batch
	switch {
	case batch < 0:
		s.sendError(id, api.ErrCodeInvalidRetain, fmt.Sprintf("batch %d is negative", batch))
		return
	case batch == 0:
		batch = defaultRetainBatch
	}
	targets := make([]*retainTarget, len(req.What))
	for i, r := range req.What {
		if r == nil {
			s.sendError(id, api.ErrCodeInvalidRetain, fmt.Sprintf("rule %d is empty", i))
			return
		}
		t, err := retainTargetOf(r)
		if err != nil {
			s.sendError(id, api.ErrCodeInvalidRetain, fmt.Sprintf("rule %d: %v", i, err))
			return
		}
		targets[i] = t
	}

	author := s.authorOr(req.Author)
	res := &api.RetainResult{Now: now.Format(time.RFC3339)}
	for i, r := range req.What {
		rr, err := s.retain(now, r, targets[i], batch, author)
		if err != nil {
			// The rules before this one have run, and their deletes are committed; an
			// error answers for the request, so it says so.
			ran := ""
			if i > 0 {
				ran = fmt.Sprintf("; rules 0..%d ran, and what they deleted is committed", i-1)
			}
			var shape *retainRuleError
			switch {
			case errors.As(err, &shape):
				s.sendError(id, api.ErrCodeInvalidRetain, fmt.Sprintf("rule %d (%s): %v%s", i, r.Path, err, ran))
			default:
				s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("rule %d (%s): %v%s", i, r.Path, err, ran))
			}
			return
		}
		res.Rules = append(res.Rules, rr)
		res.Deleted += rr.Deleted
	}
	if commit, err := s.storage.GetCurrentCommit(); err == nil {
		res.Commit = commit
	}
	s.send(&api.SessionResponse{ID: id, Result: &api.SessionResult{Retain: res}})
}

// defaultRetainBatch keeps one commit's delta, and the watch event it becomes, small
// whatever the backlog.
const defaultRetainBatch = 256

// retainRuleError is a rule that cannot mean what it says of the store it met: the
// rule and the schema disagree about the container's kind, or the container is a dense
// array. The request is wrong, not the store, and it is the same mistake next time.
type retainRuleError struct{ err error }

func (e *retainRuleError) Error() string { return e.err.Error() }
func (e *retainRuleError) Unwrap() error { return e.err }

func ruleErrorf(format string, args ...any) error {
	return &retainRuleError{err: fmt.Errorf(format, args...)}
}

// retainTarget is a rule, read: the container its items are the children of, the kind
// its items are named by, the path of an item's timestamp, and how old it may be.
type retainTarget struct {
	container string
	items     kpath.EntryKind
	age       *kpath.KPath
	after     time.Duration
}

// retainTargetOf reads a rule, and is where a rule is refused: a path that names one
// node, or a set of them at more than one step, or the positions of a dense array; an
// age that is not a field path; a match that is not an object pattern, or that names the
// age field; a duration that is not one.
func retainTargetOf(r *api.RetainRule) (*retainTarget, error) {
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
	t := &retainTarget{items: last.EntryKind()}
	if parent := kp.Parent(); parent != nil {
		t.container = parent.String()
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

	after, err := ParseDuration(r.After)
	if err != nil {
		return nil, fmt.Errorf("after %q: %w", r.After, err)
	}
	if after <= 0 {
		return nil, fmt.Errorf("after %q: an item expires after a positive duration", r.After)
	}
	t.after = after
	return t, nil
}

// retain runs one rule: reads its container as of the current commit, in the session's
// view, selects the expired items, and deletes them in batches.
func (s *Session) retain(now time.Time, rule *api.RetainRule, t *retainTarget, batch int, author string) (*api.RetainRuleResult, error) {
	res := &api.RetainRuleResult{Path: rule.Path}
	commit, err := s.storage.GetCurrentCommit()
	if err != nil {
		return nil, err
	}
	if commit == 0 {
		return res, nil
	}
	scope := s.scopeID()
	schema := s.storage.SchemaFor(scope)
	container, err := ident.CanonicalPath(schema, t.container)
	if err != nil {
		return nil, ruleErrorf("%w", err)
	}
	// The rule says what kind of thing its items are, and the schema is the authority on
	// whether the container has an identity. The two have to agree, so a rule written
	// `.*` over an array that is keyed, or `(*)` over one that is not, is a rule about a
	// container that does not exist.
	keyed := schema.Keyed(schemaPath(container))
	switch {
	case t.items == kpath.KeyEntry && !keyed:
		return nil, ruleErrorf("%s(*): the schema gives %s no identity; its items are not keyed elements", t.container, placeOrRoot(t.container))
	case t.items != kpath.KeyEntry && keyed:
		return nil, ruleErrorf("%s is keyed, so its items are elements; write the rule as %s(*)", placeOrRoot(t.container), t.container)
	}

	c, err := s.storage.Read(commit, scope, container)
	if err != nil {
		return nil, err
	}
	if c.Presence() == storage.Absent {
		c.Close()
		return res, nil
	}
	node, err := storage.Collect(c, s.readBudget)
	if err != nil {
		return nil, err
	}
	node = ir.Uncomment(node)
	if node == nil {
		return res, nil
	}
	switch node.Type {
	case ir.ObjectType:
	case ir.ArrayType:
		// The store keeps a keyed array as an object of names, so an array here is a
		// dense one, whatever the rule says its items are.
		return nil, ruleErrorf("%s is a dense array, and a position is not an element: declare it keyed (!logd-key or !logd-auto-id) and write the rule with (*)", placeOrRoot(t.container))
	default:
		return nil, ruleErrorf("%s is a %s, which has no items", placeOrRoot(t.container), node.Type)
	}

	// Select. An item with no timestamp the rule can read is kept, and counted.
	var expired []retainItem
	for i, key := range node.Fields {
		if i >= len(node.Values) {
			break
		}
		item := ir.Uncomment(node.Values[i])
		ts, ok := timestampAt(item, t.age)
		if !ok {
			res.Unreadable++
			continue
		}
		if now.Sub(ts) < t.after {
			continue
		}
		if rule.Match != nil {
			matched, err := tony.Match(item, rule.Match)
			if err != nil {
				return nil, ruleErrorf("match against %s: %w", key.String, err)
			}
			if !matched {
				continue
			}
		}
		expired = append(expired, retainItem{key: key, item: item})
	}

	// Delete, in batches.
	for len(expired) > 0 {
		n := min(batch, len(expired))
		part := expired[:n]
		expired = expired[n:]
		committed, err := s.commitRetainBatch(container, node.Tag, part, t, rule, author)
		if err != nil {
			return nil, err
		}
		if committed {
			res.Deleted += n
		} else {
			res.Skipped += n
		}
	}
	if res.Deleted > 0 {
		s.log.Info("retain: deleted expired items", "path", rule.Path, "items", res.Deleted, "after", t.after, "author", author)
	}
	return res, nil
}

// retainItem is one expired item: its name in the container, and what was read.
type retainItem struct {
	key  *ir.Node
	item *ir.Node
}

// commitRetainBatch deletes one batch of items from the container in one commit, under
// the precondition that each is as it was read. A precondition that no longer holds
// skips the batch -- the state moved under the pass -- and answers false, not an error.
func (s *Session) commitRetainBatch(container, containerTag string, batch []retainItem, t *retainTarget, rule *api.RetainRule, author string) (bool, error) {
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
		addField(match, it.key, retainPrecondition(it.item, t.age, rule.Match))
	}
	txn, err := s.storage.NewTxWithTimeout(1, s.scopeID(), 0, author)
	if err != nil {
		return false, err
	}
	p, err := txn.NewPatcher(&api.Patch{
		Match:    &api.PathData{Path: container, Data: match},
		PathData: api.PathData{Path: container, Data: patch},
	})
	if err != nil {
		return false, err
	}
	res := p.Commit()
	if res.Error != nil {
		return false, res.Error
	}
	if !res.Matched {
		s.log.Info("retain: batch skipped, the state moved under it", "path", rule.Path, "items", len(batch))
		return false, nil
	}
	if s.onCommit != nil {
		s.onCommit()
	}
	return true, nil
}

// retainPrecondition is what the delete of an item asserts about it: the timestamp the
// pass read, at its path, and the fields the rule's match names. Both are combined into
// one object pattern, which is why a match has to be one (retainTargetOf).
func retainPrecondition(item *ir.Node, age *kpath.KPath, match *ir.Node) *ir.Node {
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
