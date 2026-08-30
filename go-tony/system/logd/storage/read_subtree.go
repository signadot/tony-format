package storage

import (
	"fmt"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/patches"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/snap"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
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
// The second result says whether it managed that, and it answers (nil, false, nil)
// rather than guessing when it did not. It declines for:
//
//   - an operator ABOVE kp -- an !all, a !raw, a !delete of an ancestor -- which says
//     something about the subtree that the subtree cannot say about itself, and
//     deciding what it would have meant is the kind of guess which loses data;
//   - the root, which is not a narrowing.
//
// A scoped read narrows on the same terms. Its layer is the scope's own patches, and
// asking for them at kp is the same question the wide read asks at the root, so the two
// answer alike or one of them is wrong.
//
// A caller which is declined reads wide, which is also the only reader that can say
// what a path not fitting the document means: absent, or a segment which cannot be
// followed, which are different answers to a client. Classifying that is deliberately
// not done here -- one place does it (the server's extractPathValue), and a second
// would be a second set of error codes for the same question.
func (s *Storage) ReadSubtreeAt(kp string, commit int64, scopeID *string) (*ir.Node, bool, error) {
	started := time.Now()
	if kp == "" {
		s.readStats.note(ReadWideRoot, kp, time.Since(started))
		return nil, false, nil
	}
	if _, err := kpath.Parse(kp); err != nil {
		s.readStats.note(ReadWideBadPath, kp, time.Since(started))
		return nil, false, nil // the wide read reports what is wrong with it
	}
	node, narrowed, err := s.narrowSubtreeAt(kp, commit, scopeID)
	switch {
	case err != nil:
	case !narrowed:
		s.readStats.note(ReadWideOperator, kp, time.Since(started))
	case node == nil:
		s.readStats.note(ReadWideAbsent, kp, time.Since(started))
	default:
		s.readStats.note(ReadNarrow, kp, time.Since(started))
	}
	return node, narrowed, err
}

// ReadSubtreeRootedAt is ReadSubtreeAt with the value put back under the path it
// came from, so the result is a document of the same shape as a wide read -- with
// everything the caller did not ask for left out of it.
//
// It exists so that a caller which navigates a read can keep doing exactly that, on
// a document which cost a subtree instead of a store. It answers narrowed=false and
// a nil node when it could not narrow OR when the path holds nothing: the second is
// where a wide read's own answer is subtle (a path which is absent, versus one whose
// ancestor is a scalar, versus a document which is empty), and reproducing that
// subtlety from a narrow read is not worth getting slightly wrong. The caller reads
// wide for those, which is what it did for everything before.
func (s *Storage) ReadSubtreeRootedAt(kp string, commit int64, scopeID *string) (*ir.Node, bool, error) {
	if kp == "" {
		// Counted here rather than in ReadSubtreeAt, which this returns before
		// reaching: a read at the root is the commonest wide read there is, and a
		// report which leaves it out is a report about the wrong population.
		s.readStats.note(ReadWideRoot, kp, 0)
		return nil, false, nil
	}
	node, narrowed, err := s.ReadSubtreeAt(kp, commit, scopeID)
	if err != nil || !narrowed || node == nil {
		return nil, false, err
	}
	rooted := node
	segs := kpath.SplitAll(kp)
	for i := len(segs) - 1; i >= 0; i-- {
		name, isField := kpath.SegmentFieldName(segs[i])
		if !isField {
			s.readStats.note(ReadWideNonFieldPath, kp, 0)
			return nil, false, nil // keyed or indexed: the wide read answers those
		}
		rooted = ir.FromKeyVals([]ir.KeyVal{{Key: ir.FromString(name), Val: rooted}})
	}
	return rooted, true, nil
}

// narrowSubtreeAt: SUBTREE, replayed, in the view scopeID names -- the snapshot's path
// index seeks to kp and only the deltas which touch it are applied. See read.go for the
// axes. It reads only the subtree, or reports that it could not.
func (s *Storage) narrowSubtreeAt(kp string, commit int64, scopeID *string) (*ir.Node, bool, error) {
	baseReader, startCommit, err := s.findSubtreeBaseReader(commit, kp)
	if err != nil {
		return nil, false, err
	}
	defer baseReader.Close()

	// Baseline from the snapshot forward, then the scope over its whole history, in that
	// order: the same two ranges replayScopedAt uses, and the same reason for each. The
	// snapshot is baseline's, so baseline needs only what came after it and a scope needs
	// everything. Applying the scope last is what makes its writes shadow baseline's.
	patchNodes, err := s.patchNodesFromSegments(
		s.index.LookupSubtree(kp, &startCommit, &commit, nil), nil)
	if err != nil {
		return nil, false, err
	}
	if scopeID != nil {
		scopePatches, err := s.patchNodesFromSegments(
			s.index.LookupSubtree(kp, nil, &commit, scopeID), scopeID)
		if err != nil {
			return nil, false, err
		}
		patchNodes = append(patchNodes, scopePatches...)
	}

	projected, ok := projectPatchesAt(patchNodes, kp)
	if !ok {
		return nil, false, nil // a patch kp cannot be seen from: read wide
	}

	node, err := applyPatchesToBase(baseReader, projected)
	if err != nil {
		return nil, false, err
	}
	return node, true, nil
}

// projectPatchesAt reroots each patch as seen from kp, dropping the ones which say
// nothing about it. It reports false when one of them cannot be seen from kp at all,
// which is the caller's signal to read wide.
//
// Both layers project through here. A scope layer projected by a different rule would
// shadow different paths than the wide read does, and then neither answer is the
// definition.
//
// Re-rooting the patch re-roots its MARKER, which is the whole of what makes a projection
// applicable: the marker says where an entry applies from, and the entry now applies from
// kp. It rides in the tag chain, and on an operator node that chain is also where the
// operation lives -- so this is only safe because a merge reads a foreign label there as
// what it is rather than as the operand (see mergeop's patchUnderTagDiff, and
// 1hf5pzj6h12ksd40jdn0 for what it cost when it did not).
func projectPatchesAt(patchNodes []*ir.Node, kp string) ([]*ir.Node, bool) {
	projected := make([]*ir.Node, 0, len(patchNodes))
	for _, p := range patchNodes {
		at, marked, ok := patchAtPath(p, kp)
		if !ok {
			return nil, false
		}
		if at == nil {
			continue // this write says nothing about kp
		}
		// A marker says where an entry is applied FROM, and this has just re-rooted the
		// entry at kp, so the marker moves with it. Without that the projection carries
		// none, the streaming processor finds no patch root in it, and the write
		// contributes nothing -- a read below a write's patch root answering from the
		// snapshot, with no error to say so (1xnezrpkh12ksavvjdn0). A marker at or below
		// kp is already where it belongs: at kp the projection IS the marked node, and
		// below it the marker is inside the projected subtree.
		if marked && !tx.HasPatchRootTag(at) {
			// On a copy: the projection is a subtree of the entry this read deserialized,
			// and marking it in place would edit what the entry says.
			at = at.Clone()
			tx.MarkPatchRoot(ir.Uncomment(at))
		}
		projected = append(projected, at)
	}
	return projected, true
}

// patchAtPath answers a rooted patch as seen from kp: what it writes at or below kp,
// rerooted there. It reports false when the patch cannot be seen that way, which is
// when an operator sits above kp -- the operator's subject is the node it is written
// on, and a subtree of that node is not it.
//
// marked says a !logd-patch-root was passed on the way down, so the node returned lies
// inside a region the entry declared itself applied from. The caller re-roots that
// marker; see projectPatchesAt for why it must.
func patchAtPath(patch *ir.Node, kp string) (at *ir.Node, marked, ok bool) {
	if patch == nil {
		return nil, false, true
	}
	segs := kpath.SplitAll(kp)
	n := patch
	for _, seg := range segs {
		n = ir.Uncomment(n)
		if n == nil {
			return nil, marked, true
		}
		if tx.HasPatchRootTag(n) {
			marked = true
		}
		if hasOperator(n.Tag) {
			return nil, marked, false
		}
		if n.Type != ir.ObjectType {
			// A scalar or a list where kp descends: the write replaces the node kp
			// is inside, which is a statement about the ancestor and not about kp.
			return nil, marked, false
		}
		name, isField := kpath.SegmentFieldName(seg)
		if !isField {
			return nil, marked, false // keyed or indexed: not a plain descent
		}
		next := ir.Get(n, name)
		if next == nil {
			return nil, marked, true // the patch does not reach kp
		}
		n = next
	}
	return n, marked, true
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
