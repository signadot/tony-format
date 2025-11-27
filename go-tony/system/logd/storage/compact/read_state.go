package compact

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"sync"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/dfile"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/seq"
)

// ReadState reads the runtime state from disk
func (dc *DirCompactor) ReadState(sequence *seq.Seq, idxMu sync.Locker, idx *index.Index) ([]index.LogSegment, error) {
	dirEnts, err := os.ReadDir(dc.Dir)
	if err != nil {
		return nil, err
	}

	inputSegs := []index.LogSegment{}
	curSegs := []index.LogSegment{}
	nextLevelExists := false
	for _, de := range dirEnts {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		seg, lvl, err := paths.ParseLogSegment(name)
		if err != nil {
			continue
		}
		switch lvl {
		case dc.Level + 2:
			nextLevelExists = true
		case dc.Level + 1:
			curSegs = append(curSegs, *seg)
		case dc.Level:
			inputSegs = append(inputSegs, *seg)
		default:
		}
	}
	slices.SortFunc(inputSegs, index.LogSegCompare)
	slices.SortFunc(curSegs, index.LogSegCompare)

	// Set CurSegment to the last one
	if len(curSegs) > 0 {
		// TODO shouldn't this sort them?
		dc.CurSegment = &curSegs[len(curSegs)-1]
	}

	// Filter input segments that are already covered
	filteredInputs := []index.LogSegment{}
	for i := range inputSegs {
		seg := &inputSegs[i]
		if dc.CurSegment == nil {
			filteredInputs = append(filteredInputs, *seg)
			continue
		}
		if seg.EndCommit < dc.CurSegment.StartCommit {
			continue
		}
		if !index.WithinCommitRange(seg, dc.CurSegment) {
			filteredInputs = append(filteredInputs, *seg)
		}
	}

	last := ir.Null()
	for i := range curSegs {
		seg := &curSegs[i]
		df, err := dc.readSegment(seg, dc.Level+1)
		if err != nil {
			debug.PrintStack()
			return nil, err
		}
		tmp, err := tony.Patch(last, df.Diff)
		if err != nil {
			return nil, err
		}
		last = tmp
	}
	dc.Ref = last
	dc.Start = last
	// get inputs from CurSegment
	if dc.CurSegment != nil {
		name := paths.FormatLogSegment(dc.CurSegment, dc.Level+1, false)
		p := filepath.Join(dc.Dir, name)
		df, err := dfile.ReadDiffFile(p)
		if err != nil {
			return nil, err
		}
		dc.Inputs = df.Inputs
	}

	// Initialize Next compactor if Level+2 segments exist
	// but don't re-create it if we're in a recovery that has
	// already started it
	if dc.Next == nil && nextLevelExists {
		dc.Next = NewDirCompactor(&dc.Config, dc.Level+1, dc.Dir, dc.VirtualPath, sequence, idxMu, idx)
	}

	return filteredInputs, nil
}
