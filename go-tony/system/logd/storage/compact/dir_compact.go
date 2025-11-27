package compact

import (
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/dfile"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/seq"
)

type DirCompactor struct {
	// global config
	Config Config

	// static per-dir
	Dir         string
	VirtualPath string
	// per level per dir
	Level int

	// runtime state
	CurSegment *index.LogSegment

	Start, Ref *ir.Node
	Inputs     int

	// dir compactor at next level
	Next *DirCompactor

	// queue management
	incoming chan index.LogSegment

	// lifecycle management
	done chan struct{}
}

func NewDirCompactor(cfg *Config, lvl int, dir, vp string, sequence *seq.Seq, idxL sync.Locker, idx *index.Index) *DirCompactor {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	dc := &DirCompactor{
		Config:      *cfg,
		Dir:         dir,
		VirtualPath: vp,
		Level:       lvl,
		incoming:    make(chan index.LogSegment, bufferSize(cfg.Divisor, lvl)),
		Ref:         ir.Null(),
		Start:       ir.Null(),
		done:        make(chan struct{}),
	}
	go dc.run(sequence, idxL, idx)
	return dc
}

func (dc *DirCompactor) run(seq *seq.Seq, idxL sync.Locker, idx *index.Index) error {
	<-dc.recover(nil, seq, idxL, idx)
	for {
		var seg *index.LogSegment
		select {
		case s, ok := <-dc.incoming:
			if !ok {
				return nil
			}
			seg = &s
		case <-dc.done:
			return nil
		}
		if err := dc.processSegment(seg, seq, idxL, idx); err != nil {
			<-dc.recover(err, seq, idxL, idx)
		}
	}
}

func (dc *DirCompactor) processSegment(seg *index.LogSegment, seq *seq.Seq, idxL sync.Locker, idx *index.Index) error {
	// since recovery from an error can place us ahead of incoming,
	// be sure to skip segments we've already processed
	if dc.CurSegment != nil {
		if seg.EndCommit <= dc.CurSegment.EndCommit {
			dc.Config.Log.Debug("skipping", "segment", seg)
			return nil
		}
	}
	df, err := dc.readSegment(seg, dc.Level)
	if err != nil {
		return err
	}
	tmp, err := tony.Patch(dc.Ref, df.Diff)
	if err != nil {
		return err
	}
	diff := tony.Diff(dc.Start, tmp)
	if diff == nil {
		return nil
	}
	rotate := dc.addSegment(seg)
	if rotate {
		if err := dc.persistCurrent(seq, idxL, idx, diff); err != nil {
			return err
		}
		dc.Inputs = 0
		if dc.Next == nil {
			dc.Next = NewDirCompactor(&dc.Config, dc.Level+1, dc.Dir, dc.VirtualPath, seq, idxL, idx)
		} else {
			dc.Next.incoming <- *dc.CurSegment
		}
		dc.CurSegment = &index.LogSegment{RelPath: dc.VirtualPath}
	}
	dc.Ref = tmp
	return nil
}

func (dc *DirCompactor) recover(e error, sequence *seq.Seq, idxMu sync.Locker, idx *index.Index) chan struct{} {
	recovered := make(chan struct{})
	go dc.recoverNotify(e, sequence, idxMu, idx, recovered)
	return recovered
}

func (dc *DirCompactor) recoverNotify(e error, sequence *seq.Seq, idxMu sync.Locker, idx *index.Index, notify chan struct{}) {
	if e != nil {
		dc.Config.Log.Warn("starting recovery for", "path", dc.VirtualPath, "error", e)
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		dc.Inputs = 0
		dc.CurSegment = nil
		dc.Start = ir.Null()
		dc.Ref = ir.Null()

		inputSegs, err := dc.ReadState(sequence, idxMu, idx)
		if err != nil {
			dc.Config.Log.Warn("error reading state in recover: trying again", "error", err)
			goto again
		}
		for i := range inputSegs {
			if err := dc.processSegment(&inputSegs[i], sequence, idxMu, idx); err != nil {
				dc.Config.Log.Warn("error processing segments in recover: trying again", "error", err)
				goto again
			}
		}
		close(notify)
		return
	again:
		select {
		case <-ticker.C:
		case <-dc.done:
			return
		}
	}
}

func (dc *DirCompactor) addSegment(seg *index.LogSegment) bool {
	if dc.CurSegment == nil {
		dc.CurSegment = &index.LogSegment{
			StartCommit: seg.StartCommit,
			StartTx:     seg.StartTx,
			RelPath:     dc.VirtualPath,
		}
	}
	dc.CurSegment.EndCommit = seg.EndCommit
	dc.CurSegment.EndTx = seg.EndTx
	dc.Inputs++
	return dc.Inputs >= dc.Config.Divisor
}

func (dc *DirCompactor) persistCurrent(sequence *seq.Seq, idxL sync.Locker, idx *index.Index, diff *ir.Node) error {
	// 1. Allocate txSeq
	txSeq, err := sequence.NextTxSeq()
	if err != nil {
		return err
	}
	// 2. Write pending
	seg := &index.LogSegment{
		StartCommit: 0,
		EndCommit:   0,
		StartTx:     dc.CurSegment.StartTx,
		EndTx:       dc.CurSegment.EndTx,
		RelPath:     dc.VirtualPath,
	}

	filename := paths.FormatLogSegment(seg, dc.Level+1, true)
	df := &dfile.DiffFile{
		Seq:     txSeq,
		Diff:    diff,
		Inputs:  dc.Inputs,
		Pending: true,
	}
	err = dfile.WriteDiffFile(filepath.Join(dc.Dir, filename), df)
	if err != nil {
		return err
	}

	// 3. Commit
	sequence.Lock()
	defer sequence.Unlock()
	commit, err := sequence.NextCommitLocked()
	if err != nil {
		return err
	}

	seg.StartCommit = dc.CurSegment.StartCommit
	seg.EndCommit = commit

	if err := dfile.CommitPending(dc.Dir, seg, dc.Level+1, commit); err != nil {
		return err
	}

	// 4. Index
	idxL.Lock()
	defer idxL.Unlock()
	idx.Add(seg)

	// update current to have start commit if 0
	if dc.CurSegment.StartCommit == 0 {
		dc.CurSegment.StartCommit = commit
	}
	dc.CurSegment.EndCommit = commit
	dc.CurSegment.EndTx = txSeq
	// todo remove old .diff files
	return nil
}

func (dc *DirCompactor) readSegment(seg *index.LogSegment, lvl int) (*dfile.DiffFile, error) {
	name := paths.FormatLogSegment(seg, lvl, false)
	p := filepath.Join(dc.Dir, name)
	return dfile.ReadDiffFile(p)
}

func bufferSize(divisor, level int) int {
	const N = 3
	if level >= N {
		return 1
	}
	// pow(divisor, N - level)
	size := 1
	for i := 0; i < N-level; i++ {
		size *= divisor
	}
	return size
}
