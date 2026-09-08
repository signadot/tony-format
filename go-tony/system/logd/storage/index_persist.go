package storage

import (
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

	// The regions the file lacks, and the manifest: what makes the regions written
	// since the last persist evictable (index.Persist).
	if err := p.index.Persist(p.generations()); err != nil {
		p.logger.Error("failed to persist index", "error", err)
		return
	}

	p.lastPersisted.Store(maxCommit)
	p.logger.Debug("index persisted", "commit", maxCommit)
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
