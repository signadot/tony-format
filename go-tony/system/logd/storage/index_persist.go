package storage

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// IndexPersister handles periodic background persistence of the index.
// It serializes persist operations and tracks the last persisted commit
// to avoid redundant writes.
type IndexPersister struct {
	mu            sync.Mutex
	lastPersisted atomic.Int64
	interval      int64
	wg            sync.WaitGroup
	logger        *slog.Logger
	index         *index.Index
	generations   func() map[string]int64
}

// NewIndexPersister creates a new IndexPersister.
// interval is the number of commits between persists (0 disables periodic persistence).
func NewIndexPersister(idx *index.Index, generations func() map[string]int64, interval int64, logger *slog.Logger) *IndexPersister {
	return &IndexPersister{
		interval:    interval,
		logger:      logger,
		index:       idx,
		generations: generations,
	}
}

// MaybePersist triggers an async persist if enough commits have occurred.
// Returns immediately; the persist happens in a background goroutine.
func (p *IndexPersister) MaybePersist(commit int64) {
	if p.interval <= 0 {
		return
	}
	if commit-p.lastPersisted.Load() < p.interval {
		return
	}

	p.wg.Add(1)
	go p.persistAsync(commit)
}

// persistAsync persists the index in the background.
func (p *IndexPersister) persistAsync(commit int64) {
	defer p.wg.Done()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Skip if another goroutine already persisted past this commit
	if commit <= p.lastPersisted.Load() {
		return
	}

	maxCommit := p.getMaxCommit()

	if err := p.persist(); err != nil {
		p.logger.Error("failed to persist index", "error", err)
		return
	}

	p.lastPersisted.Store(maxCommit)
	p.logger.Debug("index persisted", "commit", maxCommit)
}

// persist writes the regions the file lacks, and the manifest: what makes the regions
// written since the last persist evictable (index.Persist). Each region it writes leaves
// the region's previous record as garbage, and a store that stays open collects it
// without end -- a compaction's re-index dirties every survivor's region, so each cycle
// appends an index of the survivors (1xa86mrnh12krzswvdn0) -- so once the garbage
// outgrows the current records the file is rewritten without it (index.Rewrite). The
// rule bounds the file at twice what it describes and the copying at the appending.
func (p *IndexPersister) persist() error {
	if err := p.index.Persist(p.generations()); err != nil {
		return err
	}
	garbage, live := p.index.RegionsGarbage()
	if garbage <= live {
		return nil
	}
	if err := p.index.Rewrite(p.generations()); err != nil {
		return fmt.Errorf("rewriting the regions file: %w", err)
	}
	p.logger.Info("index regions rewritten", "dropped", garbage, "kept", live)
	return nil
}

// Hold keeps every persist from running until the returned func is called. Compaction
// holds it from the swap that bumps a file's generation to the end of its re-index: a
// manifest written in between names the new generation over the survivors' old
// positions, the next open trusts it, and every read then fails "read interrupted by
// compaction" until the manifest is deleted by hand (05d8w3cjh12kswb1msn0, item 5).
func (p *IndexPersister) Hold() func() {
	p.mu.Lock()
	return p.mu.Unlock
}

// Persist writes the index now: what MaybePersist does, in the caller's goroutine, and
// after any hold is released.
func (p *IndexPersister) Persist() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.persist(); err != nil {
		return err
	}
	p.lastPersisted.Store(p.getMaxCommit())
	return nil
}

// getMaxCommit returns the highest commit in the index, from the regions' headers.
func (p *IndexPersister) getMaxCommit() int64 {
	commit, _, ok := p.index.MaxCommit()
	if !ok {
		return -1
	}
	return commit
}

// Close waits for any pending persist to complete.
func (p *IndexPersister) Close() {
	p.wg.Wait()
}

// SetLastPersisted sets the last persisted commit (used during init).
func (p *IndexPersister) SetLastPersisted(commit int64) {
	p.lastPersisted.Store(commit)
}
