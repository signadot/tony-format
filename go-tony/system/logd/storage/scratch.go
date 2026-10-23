package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// Scratch is the store's second document: one Tony node beside the log, with no
// commits, no history, no index, no scopes, no schema, no watches and no transactions.
// It holds state overwritten too often to keep a history of (r482sz5rh12ksj0cvdn0),
// and has two operations, write a patch at a path and read the value at a path.
//
// A write patches the document in memory, in place, under one mutex, and is
// acknowledged then. A flusher ticks every scratchFlush and, when anything changed,
// copies the document under the mutex, encodes the copy outside it, and renames it over
// scratch.tony; one flush lock from the copy to the rename keeps two flushes from
// sharing a temp file or landing out of order. So the file is always the document as
// it stood after some write k: a crash loses a suffix of the newest writes, never a
// hole. Scratch promises no more than that about durability.
//
// See docs/scratch_plan.md for the design and what it rejected.

// scratchFile is the scratch document's file, under the store's root.
const scratchFile = "scratch.tony"

// DefaultScratchFlush is how often the scratch document is written to disk when it
// has changed, when nothing configures it.
const DefaultScratchFlush = time.Second

// ScratchApplyError is a scratch write whose patch does not apply to what is there: a
// dense index into no array, a mergeop over a value it cannot take, a tag that is no
// mergeop. The document is unchanged, and it is the same mistake next time.
type ScratchApplyError struct {
	Path string
	Err  error
}

func (e *ScratchApplyError) Error() string {
	return fmt.Sprintf("the scratch write at %q does not apply: %v", e.Path, e.Err)
}

func (e *ScratchApplyError) Unwrap() error { return e.Err }

// ScratchPathError is a scratch read whose path meets a shape it cannot step into: an
// index into an object, a field under a string, an index past the end.
type ScratchPathError struct {
	Path string
	Err  error
}

func (e *ScratchPathError) Error() string {
	return fmt.Sprintf("the scratch path %q: %v", e.Path, e.Err)
}

func (e *ScratchPathError) Unwrap() error { return e.Err }

type scratch struct {
	mu    sync.Mutex // the document
	doc   *ir.Node   // nil = empty; owned: patched in place under mu
	dirty bool       // changed since the last successful flush

	flushMu sync.Mutex // one flush at a time, copy through rename
	path    string     // <root>/scratch.tony
	logger  *slog.Logger

	// The flusher, once started. ticker is reset by a later SetScratchFlush; stop is
	// closed by close, and stopped by the flusher when it has exited.
	ticker  *time.Ticker
	period  time.Duration
	stop    chan struct{}
	stopped chan struct{}
}

// openScratch reads the scratch document from root, or starts empty when there is
// none. A file that does not parse refuses the open: scratch is one whole file, so
// stepping over it would mean starting empty, which loses everything without a word,
// where refusing loses nothing and names the file for the operator to move aside.
func openScratch(root string, logger *slog.Logger) (*scratch, error) {
	path := filepath.Join(root, scratchFile)
	sc := &scratch{path: path, logger: logger}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return sc, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scratch: %w", err)
	}
	if len(data) == 0 {
		return sc, nil
	}
	doc, err := parse.Parse(data, parse.ParseComments(true))
	if err != nil {
		return nil, fmt.Errorf("scratch file %s does not parse: %w; move it aside to start scratch empty", path, err)
	}
	sc.doc = doc
	return sc, nil
}

// scratchPath refuses a path that does not name one place: one that does not parse,
// or holds a wildcard or `..`. The server refuses these at its door too; this keeps
// the store right when called directly.
func scratchPath(path string) error {
	kp, err := kpath.Parse(path)
	if err != nil {
		return err
	}
	if kp.HasWild() {
		return fmt.Errorf("%q names a set, and a scratch path names one place", path)
	}
	return nil
}

// ScratchWrite patches the scratch document at path with data, as a merge patch, and
// answers once it is applied in memory. The operations which execute are refused, as
// the document refuses them, and a patch that does not apply is refused: both are a
// ScratchApplyError, and leave the document as it was. Path "" patches the whole
// document: !delete there empties it. A result that is null is stored as empty, so
// scratch is never a bare null. A write after Close is refused: the flusher has
// stopped, and an acknowledged write would never reach disk.
func (s *Storage) ScratchWrite(path string, data *ir.Node) error {
	if err := scratchPath(path); err != nil {
		return err
	}
	if data == nil {
		return fmt.Errorf("scratch write at %q: data is required", path)
	}
	if op, found := mergeop.FindUnsafe(data); found {
		return &ScratchApplyError{Path: path, Err: fmt.Errorf("!%s calls out to the system, "+
			"and a scratch write states a value", op)}
	}
	if s.closing.Load() {
		return fmt.Errorf("scratch write at %q: the store is closing", path)
	}
	rooted, err := tx.RootPatchAt(path, data)
	if err != nil {
		return &ScratchApplyError{Path: path, Err: err}
	}
	sc := s.scratch
	sc.mu.Lock()
	defer sc.mu.Unlock()
	base := sc.doc
	if base == nil {
		base = ir.Null()
	}
	// PatchOwned takes the document rather than copying it: a write costs what it
	// touches, not the size of scratch. Nothing else holds the document -- a read
	// answers a copy, the flusher takes one -- which is what makes that safe.
	res, err := tony.PatchOwned(base, rooted, mergeop.Comments(true), mergeop.RejectUnsafe(true))
	if err != nil {
		// A failed apply may have re-parented children into the result it then
		// dropped; Clone rebuilds the links, so the next write patches a whole
		// document. Rare, and O(size) only then.
		if sc.doc != nil {
			sc.doc = sc.doc.Clone()
		}
		return &ScratchApplyError{Path: path, Err: err}
	}
	if u := ir.Uncomment(res); u != nil && u.Type == ir.NullType {
		res = nil
	}
	sc.doc = res
	sc.dirty = true
	return nil
}

// ScratchRead answers the scratch document at path as it stands now, the whole
// subtree, as a copy. Absent is (nil, nil), the package's idiom: a read before any
// write is routine. A path that meets the wrong shape is a ScratchPathError.
func (s *Storage) ScratchRead(path string) (*ir.Node, error) {
	if err := scratchPath(path); err != nil {
		return nil, err
	}
	sc := s.scratch
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.doc == nil {
		return nil, nil
	}
	n, err := sc.doc.GetKPathWith(path, ir.WithComments(true))
	if err != nil {
		return nil, &ScratchPathError{Path: path, Err: err}
	}
	return n, nil
}

// SetScratchFlush sets how often the scratch document is written to disk when it has
// changed, and starts the flusher; zero or less means the default. A later call only
// resets the period. A store that never calls it has no flusher, and Close still
// flushes.
func (s *Storage) SetScratchFlush(d time.Duration) {
	if d <= 0 {
		d = DefaultScratchFlush
	}
	sc := s.scratch
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.period = d
	if sc.ticker != nil {
		sc.ticker.Reset(d)
		return
	}
	sc.ticker = time.NewTicker(d)
	sc.stop = make(chan struct{})
	sc.stopped = make(chan struct{})
	go sc.run(s, sc.ticker, sc.stop, sc.stopped)
}

// ScratchFlush is the flusher's period, or zero when no flusher runs.
func (s *Storage) ScratchFlush() time.Duration {
	sc := s.scratch
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.ticker == nil {
		return 0
	}
	return sc.period
}

// run is the flusher: each tick flushes what changed; a failed flush is logged, and
// the next tick tries again, the document staying in memory meanwhile. It is handed
// what it watches rather than reading the struct, which close is changing.
func (sc *scratch) run(s *Storage, ticker *time.Ticker, stop, stopped chan struct{}) {
	defer close(stopped)
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if err := sc.flushNow(s.GetDurability() == DurabilitySync); err != nil {
				sc.logger.Error("scratch flush failed; the document stays in memory and the next tick retries",
					"file", sc.path, "error", err)
			}
		}
	}
}

// flushNow writes the scratch document to disk if it changed since the last flush.
// The flush lock is held from the copy to the rename, so flushes never overlap; the
// document lock only while the copy is taken, so writes and reads go on meanwhile.
func (sc *scratch) flushNow(fsync bool) error {
	sc.flushMu.Lock()
	defer sc.flushMu.Unlock()

	sc.mu.Lock()
	if !sc.dirty {
		sc.mu.Unlock()
		return nil
	}
	var snapshot *ir.Node
	if sc.doc != nil {
		snapshot = sc.doc.Clone()
	}
	sc.dirty = false
	sc.mu.Unlock()

	if err := sc.write(snapshot, fsync); err != nil {
		sc.mu.Lock()
		sc.dirty = true
		sc.mu.Unlock()
		return err
	}
	return nil
}

// write puts doc in the scratch file, whole or not at all: encoded to a temp file and
// renamed over the one before. Under fsync the file is synced before the rename and
// the directory after it. A nil doc is an empty file, which reads back as empty.
func (sc *scratch) write(doc *ir.Node, fsync bool) (err error) {
	tmp := sc.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(tmp)
		}
	}()
	if doc != nil {
		if err = encode.Encode(doc, f, encode.EncodeComments(true)); err != nil {
			f.Close()
			return err
		}
	}
	if fsync {
		if err = f.Sync(); err != nil {
			f.Close()
			return err
		}
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, sc.path); err != nil {
		return err
	}
	if fsync {
		dir, derr := os.Open(filepath.Dir(sc.path))
		if derr != nil {
			return derr
		}
		defer dir.Close()
		return dir.Sync()
	}
	return nil
}

// close stops the flusher, waits for a flush in flight, and flushes once more.
func (sc *scratch) close(fsync bool) error {
	sc.mu.Lock()
	stop, stopped, ticker := sc.stop, sc.stopped, sc.ticker
	sc.stop = nil // a second close finds no flusher to stop
	sc.mu.Unlock()
	if stop != nil {
		close(stop)
		<-stopped
		ticker.Stop()
		sc.mu.Lock()
		sc.ticker = nil
		sc.mu.Unlock()
	}
	return sc.flushNow(fsync)
}
