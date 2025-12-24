package storage

import (
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// IndexPersister handles periodic background persistence of the index.
// It serializes persist operations and tracks the last persisted commit
// to avoid redundant writes.
type IndexPersister struct {
	mu            sync.Mutex
	lastPersisted int64
	interval      int64
	wg            sync.WaitGroup
	logger        *slog.Logger
	root          string
	index         *index.Index
}

// NewIndexPersister creates a new IndexPersister.
// interval is the number of commits between persists (0 disables periodic persistence).
func NewIndexPersister(root string, idx *index.Index, interval int64, logger *slog.Logger) *IndexPersister {
	return &IndexPersister{
		interval: interval,
		logger:   logger,
		root:     root,
		index:    idx,
	}
}

// MaybePersist triggers an async persist if enough commits have occurred.
// Returns immediately; the persist happens in a background goroutine.
func (p *IndexPersister) MaybePersist(commit int64) {
	if p.interval <= 0 {
		return
	}
	if commit-p.lastPersisted < p.interval {
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
	if commit <= p.lastPersisted {
		return
	}

	maxCommit := p.getMaxCommit()
	indexPath := filepath.Join(p.root, "index.gob")

	if err := index.StoreIndexWithMetadata(indexPath, p.index, maxCommit); err != nil {
		p.logger.Error("failed to persist index", "error", err)
		return
	}

	p.lastPersisted = maxCommit
	p.logger.Debug("index persisted", "commit", maxCommit)
}

// getMaxCommit returns the highest commit in the index.
func (p *IndexPersister) getMaxCommit() int64 {
	p.index.RLock()
	defer p.index.RUnlock()

	segments := p.index.LookupRange("", nil, nil)
	var maxCommit int64 = -1
	for _, seg := range segments {
		if seg.EndCommit > maxCommit {
			maxCommit = seg.EndCommit
		}
	}
	return maxCommit
}

// Close waits for any pending persist to complete.
func (p *IndexPersister) Close() {
	p.wg.Wait()
}

// SetLastPersisted sets the last persisted commit (used during init).
func (p *IndexPersister) SetLastPersisted(commit int64) {
	p.lastPersisted = commit
}
