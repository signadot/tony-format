package storage

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/patches"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/snap"
)

// ReadSubtreeAt reads the value at kp, rather than the document which contains it.
//
// ReadStateAt takes a path and reads at the root regardless: it replays the whole
// delta over the whole snapshot and materializes the whole document, so a caller
// after one entity out of a tree pays for the tree (ap8ddvp2h12krd43gdn0). This
// reads the snapshot's own subtree at kp -- the snapshot is indexed by path and can
// be streamed from an offset near it -- and applies only the writes which touch that
// subtree, projected to it.
//
// The second result says whether it managed that. A patch cannot always be projected
// to kp: an operator ABOVE kp (an !all, a !raw, a !delete of an ancestor) says
// something about the subtree which the subtree cannot say about itself, and
// deciding what it would have meant is the kind of guess that loses data. So the
// read falls back to the whole document and navigates to kp, and answers false --
// the same value, at the old cost, which is the honest degradation.
//
// A scoped read falls back for now: a scope is read as raw op-preserving patches
// over the baseline in one pass (readScopedStateAt), and narrowing it wants that
// pass to be path-aware first.
func (s *Storage) ReadSubtreeAt(kp string, commit int64, scopeID *string) (*ir.Node, bool, error) {
	if kp == "" || scopeID != nil {
		n, err := s.ReadStateAt(kp, commit, scopeID)
		if err != nil {
			return nil, false, err
		}
		return n, false, nil
	}
	if _, err := kpath.Parse(kp); err != nil {
		return nil, false, fmt.Errorf("invalid path %q: %w", kp, err)
	}

	node, narrowed, err := s.readSubtreeNarrow(kp, commit)
	if err != nil {
		return nil, false, err
	}
	if narrowed {
		return node, true, nil
	}
	return s.readSubtreeWide(kp, commit)
}

// readSubtreeWide is the fallback: the whole document, navigated to kp.
func (s *Storage) readSubtreeWide(kp string, commit int64) (*ir.Node, bool, error) {
	full, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		return nil, false, err
	}
	if full == nil {
		return nil, false, nil
	}
	at, err := full.GetKPath(kp)
	if err != nil {
		return nil, false, fmt.Errorf("navigate to %q: %w", kp, err)
	}
	return at, false, nil
}

// readSubtreeNarrow reads only the subtree, or reports that it could not.
func (s *Storage) readSubtreeNarrow(kp string, commit int64) (*ir.Node, bool, error) {
	baseReader, startCommit, err := s.findSubtreeBaseReader(commit, kp)
	if err != nil {
		return nil, false, err
	}
	defer baseReader.Close()

	segments := s.index.LookupSubtree(kp, &startCommit, &commit, nil)
	patchNodes, err := s.patchNodesFromSegments(segments, nil)
	if err != nil {
		return nil, false, err
	}

	projected := make([]*ir.Node, 0, len(patchNodes))
	for _, p := range patchNodes {
		at, ok := patchAtPath(p, kp)
		if !ok {
			return nil, false, nil // an operator above kp: fall back
		}
		if at == nil {
			continue // this write says nothing about kp
		}
		projected = append(projected, at)
	}

	node, err := applyPatchesToBase(baseReader, projected)
	if err != nil {
		return nil, false, err
	}
	return node, true, nil
}

// patchAtPath answers a rooted patch as seen from kp: what it writes at or below kp,
// rerooted there. It reports false when the patch cannot be seen that way, which is
// when an operator sits above kp -- the operator's subject is the node it is written
// on, and a subtree of that node is not it.
func patchAtPath(patch *ir.Node, kp string) (*ir.Node, bool) {
	if patch == nil {
		return nil, true
	}
	segs := kpath.SplitAll(kp)
	n := patch
	for _, seg := range segs {
		n = ir.Uncomment(n)
		if n == nil {
			return nil, true
		}
		if hasOperator(n.Tag) {
			return nil, false
		}
		if n.Type != ir.ObjectType {
			// A scalar or a list where kp descends: the write replaces the node kp
			// is inside, which is a statement about the ancestor and not about kp.
			return nil, false
		}
		name, isField := kpath.SegmentFieldName(seg)
		if !isField {
			return nil, false // keyed or indexed: not a plain descent
		}
		next := ir.Get(n, name)
		if next == nil {
			return nil, true // the patch does not reach kp
		}
		n = next
	}
	return n, true
}

// hasOperator reports whether a tag names a merge operation, which is what makes a
// node's subtree unable to speak for it.  Presentation and data tags are not
// operations: they travel with the value and say nothing about how it merges.
func hasOperator(tag string) bool {
	for t := tag; t != ""; {
		head, _, rest := ir.TagArgs(t)
		if head == "" {
			return false
		}
		if mergeop.Lookup(head[1:]) != nil {
			return true
		}
		if rest == t {
			return false
		}
		t = rest
	}
	return false
}

// findSubtreeBaseReader is findSnapshotBaseReader, reading the snapshot AT kp.
//
// The snapshot segment is still looked up at the root, because that is where a
// snapshot is indexed -- looking it up at the read's path finds nothing and silently
// replays from commit 0 (bvm163tyh12krwcqcsn0). What kp narrows is the read WITHIN
// the snapshot: its index maps paths to offsets, so the events of one subtree can be
// streamed without materializing the rest.
func (s *Storage) findSubtreeBaseReader(commit int64, kp string) (patches.EventReadCloser, int64, error) {
	snapSeg, ok := s.baselineSnapshotSegment(commit)
	if !ok {
		return patches.NewEmptyEventReader(), 0, nil
	}

	entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(snapSeg.LogFile), snapSeg.LogPosition, snapSeg.LogFileGeneration)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read snapshot entry: %w", err)
	}
	if entry.SnapPos == nil {
		return nil, 0, fmt.Errorf("snapshot entry missing SnapPos")
	}
	snapReader, err := s.dLog.OpenReaderAt(dlog.LogFileID(snapSeg.LogFile), *entry.SnapPos, snapSeg.LogFileGeneration)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open snapshot reader: %w", err)
	}
	snapshot, err := snap.Open(snapReader)
	if err != nil {
		snapReader.Close()
		return nil, 0, fmt.Errorf("failed to open snapshot: %w", err)
	}
	eventReader, err := snapshot.ReadPathEventReader(kp)
	if err != nil {
		snapshot.Close()
		return nil, 0, fmt.Errorf("error creating event reader at %q: %w", kp, err)
	}
	return &snapshotEventReadCloser{snapshot: snapshot, reader: eventReader}, snapSeg.StartCommit + 1, nil
}
