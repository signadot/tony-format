package tx

import (
	"errors"
	"fmt"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A write at an array index is POSITIONAL: `votes[2]` names the element which is
// there. A field is different -- writing a.b.c creates whatever is missing on the
// way -- and an index cannot be, because there is no such thing as the third
// element of a two-element array.
//
// logd accepted any index at all. The patch committed, and every read after it
// died trying to apply it -- including reads of entities the write never touched,
// because they all replay through the same log -- and no client could repair it: a
// delete of the entity is a LATER patch, so the read still dies on the way past the
// bad one. It also stopped the store snapshotting, so it could no longer compact
// (7cdvym1fh12ksmd5g5n0).
//
// The array's length is a fact at the moment the write is SUBMITTED, and that is
// where a client can be told about it, so that is where this is checked. What each
// index segment has to be true of depends on what the write does at the end of the
// path:
//
//	patch, delete   the element must exist                 i < len
//	insert          the position must be one the array	   i <= len
//	                has or ends at
//
// An index which is not the last segment always asks the first question: a write
// cannot insert THROUGH a position on its way to something deeper.
//
// The check is paid only by a path which names an index. A write at a field --
// which is nearly all of them -- reads nothing and costs a scan of its own path.
func hasArrayIndex(path string) bool {
	if path == "" || !strings.ContainsRune(path, '[') {
		return false
	}
	for _, seg := range kpath.SplitAll(path) {
		if kp, err := kpath.Parse(seg); err == nil && kp != nil && kp.Index != nil {
			return true
		}
	}
	return false
}

// NoSuchElementError is what a positional write gets when its index names no
// element. It is the CLIENT's mistake, not the store's failure, and it is typed so
// a caller can say so: the session used to report every NewPatcher failure as a
// storage_error or a tx_full, which tells a client to retry something that will
// never work.
type NoSuchElementError struct {
	Path string // the write path, as the client gave it
	Why  string
}

func (e *NoSuchElementError) Error() string { return e.Why }

func noSuchElement(path, format string, args ...any) error {
	return &NoSuchElementError{Path: path, Why: fmt.Sprintf(format, args...)}
}

// checkArrayWritePath refuses a write whose index segments name no element of doc.
// doc is the current state, rooted at the document root, as ReadStateAt returns it.
func checkArrayWritePath(path string, data, doc *ir.Node) error {
	segs := kpath.SplitAll(path)
	inserts := writeInserts(data)
	prefix := ""
	for i, seg := range segs {
		kp, err := kpath.Parse(seg)
		if err != nil {
			return fmt.Errorf("path %q: %w", path, err)
		}
		if kp == nil || kp.Index == nil {
			prefix = joinSegment(prefix, seg)
			continue
		}
		idx := *kp.Index
		arr, err := valueAt(doc, prefix)
		if err != nil {
			return fmt.Errorf("path %q: %w", path, err)
		}
		last := i == len(segs)-1
		switch {
		case arr == nil || arr.Type == ir.NullType:
			// An insert at 0 could reasonably CREATE the array, which is what a
			// field write does. It cannot yet: arraydiff refuses a base which is
			// not already an array (7ftjmf1ah12kranxg5n0), and letting the write
			// through would store a patch no read can apply.
			return noSuchElement(path, "%s names nothing, so %s names no element: an array must "+
				"exist before an element of it can be written", at(prefix), path)
		case arr.Type != ir.ArrayType:
			return noSuchElement(path, "%s is %s, not an array, so %s names no element",
				at(prefix), arr.Type, path)
		}
		n := len(arr.Values)
		if last && inserts {
			if idx > n {
				return noSuchElement(path, "%s holds %d elements, so an insert at %s can be at "+
					"most [%d]: an insert goes before an element which is there, or at the end",
					at(prefix), n, path, n)
			}
			continue
		}
		if idx >= n {
			return noSuchElement(path, "%s holds %d elements, so %s names no element: an index "+
				"names the element which is there, and %d is past the end",
				at(prefix), n, path, idx)
		}
		prefix = joinSegment(prefix, seg)
	}
	return nil
}

// writeInserts reports whether the write's operation is an insert, which is the one
// operation that addresses a position rather than an element. The patch root is not
// marked yet when this is asked, so the chain is the client's own -- and the op is
// found the way mergeop finds it, past any label which is not an operation.
func writeInserts(data *ir.Node) bool {
	node := ir.Uncomment(data)
	if node == nil {
		return false
	}
	_, op, _, _, err := mergeop.SplitChild(node)
	return err == nil && op == "insert"
}

// valueAt returns the single value at kp, or nil when the path names nothing. An
// empty kp is the document root.
func valueAt(doc *ir.Node, kp string) (*ir.Node, error) {
	if kp == "" {
		return doc, nil
	}
	if doc == nil {
		return nil, nil
	}
	nodes, err := doc.ListKPath(nil, kp)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, nil
	}
	return nodes[0], nil
}

// joinSegment appends a kpath segment to a path. SplitAll returns segments bare --
// "votes", "\"1\"", "[2]" -- so a field needs the '.' the split took off and an
// index or sparse index does not.
func joinSegment(path, seg string) string {
	if seg == "" {
		return path
	}
	if path == "" || seg[0] == '[' || seg[0] == '{' {
		return path + seg
	}
	return path + "." + seg
}

func at(kp string) string {
	if kp == "" {
		return "the document root"
	}
	return kp
}

// checkArrayWrite reads the current state and holds the patch's path to it. It
// reads nothing at all unless the path names an index, which is what keeps the
// ordinary write -- at a field -- paying nothing for this.
//
// The read is ReadStateAt rather than the head MatchStateAt serves: this runs with
// no lock held, and the head may only be read under the commit lock. That is the
// right trade here, because the answer does not have to be the last word -- the
// commit re-asks it under the lock, where the head makes it cheap.
func (co *txCoord) checkArrayWrite(p *api.Patch) error {
	if p == nil || p.Data == nil || !hasArrayIndex(p.Path) {
		return nil
	}
	commit, err := co.commitOps.GetCurrentCommit()
	if err != nil {
		return fmt.Errorf("cannot check %q against current state: %w", p.Path, err)
	}
	doc, err := co.commitOps.ReadStateAt("", commit, co.Scope())
	if err != nil {
		return fmt.Errorf("cannot read current state to check %q: %w", p.Path, err)
	}
	return checkArrayWritePath(p.Path, p.Data, doc)
}

// CheckArrayWritesAt holds every positional write in the transaction to the state
// at commit. The commit path calls it under the commit lock, where MatchStateAt
// answers from the stepped head, so an array which lost its element between the
// write's submission and its commit is caught before the patch is stored rather
// than by every read afterwards.
func CheckArrayWritesAt(patches []*PatcherData, read func() (*ir.Node, error)) error {
	var doc *ir.Node
	loaded := false
	for _, pd := range patches {
		if pd == nil || pd.API == nil || pd.API.Data == nil || !hasArrayIndex(pd.API.Path) {
			continue
		}
		if !loaded {
			var err error
			if doc, err = read(); err != nil {
				return fmt.Errorf("cannot read current state to check %q: %w", pd.API.Path, err)
			}
			loaded = true
		}
		if err := checkArrayWritePath(pd.API.Path, pd.API.Data, doc); err != nil {
			// It passed when it was submitted, so what changed is the array, not the
			// request. Say which, and keep the type: the write is still one the
			// client cannot make now.
			var nse *NoSuchElementError
			if errors.As(err, &nse) {
				nse.Why = "the array changed between this write and its commit: " + nse.Why
			}
			return err
		}
	}
	return nil
}
