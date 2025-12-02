package storage

import (
	"math"
	"sort"
	"sync"

	"github.com/signadot/tony-format/go-tony/ir"
)

// cacheKey uniquely identifies a cached computed state.
type cacheKey struct {
	path   string
	commit int64
}

// cacheEntry represents a cached value with heat tracking.
type cacheEntry[V any] struct {
	Value         V
	EndCommit     int64 // Commit number this value represents
	estimatedSize int64 // Estimated memory size in bytes

	// Heat tracking
	frequency  float64 // Access count (with decay)
	lastAccess int64   // Commit number of last access
}

// stateCache is a two-tier heat-based cache for document states.
// Tier 1: Compacted states (one per path, from compactor)
// Tier 2: Computed states (multiple per path, from ReadStateAt)
type stateCache struct {
	mu sync.RWMutex

	// Tier 1: Compacted states (path -> entry)
	compacted map[string]*cacheEntry[*ir.Node]

	// Tier 2: Computed states ((path, commit) -> entry)
	computed map[cacheKey]*cacheEntry[*ir.Node]

	// Configuration
	sizeThreshold int64 // Baseline for size penalty (e.g., 10MB)
	softLimit     int64 // Soft memory limit (start evicting)
	hardLimit     int64 // Hard memory limit (aggressive eviction)
	currentCommit int64 // Current commit for recency calculation

	// Statistics
	totalSize int64 // Total estimated memory usage
}

// newStateCache creates a new state cache with the given configuration.
func newStateCache(sizeThreshold, softLimit, hardLimit int64) *stateCache {
	return &stateCache{
		compacted:     make(map[string]*cacheEntry[*ir.Node]),
		computed:      make(map[cacheKey]*cacheEntry[*ir.Node]),
		sizeThreshold: sizeThreshold,
		softLimit:     softLimit,
		hardLimit:     hardLimit,
	}
}

// updateCurrentCommit updates the current commit for recency calculations.
func (c *stateCache) updateCurrentCommit(commit int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentCommit = commit
}

// computeHeat calculates the heat score for a cache entry.
// heat = (frequency * recency_weight) / size_penalty
func (e *cacheEntry[V]) computeHeat(currentCommit, sizeThreshold int64) float64 {
	// Recency: decay with commit distance
	distance := currentCommit - e.lastAccess
	if distance < 1 {
		distance = 1
	}
	recencyWeight := 1.0 / math.Log(float64(distance)+1.0)

	// Size penalty: larger documents are penalized
	sizePenalty := 1.0 + float64(e.estimatedSize)/float64(sizeThreshold)

	// Heat = frequency * recency / size_penalty
	return e.frequency * recencyWeight / sizePenalty
}

// getCompacted retrieves a compacted state from the cache.
func (c *stateCache) getCompacted(path string) *cacheEntry[*ir.Node] {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.compacted[path]
}

// getComputed retrieves a computed state from the cache.
func (c *stateCache) getComputed(path string, commit int64) *cacheEntry[*ir.Node] {
	c.mu.RLock()
	defer c.mu.RUnlock()
	key := cacheKey{path: path, commit: commit}
	entry := c.computed[key]
	if entry != nil {
		// Update access tracking (but don't increment frequency yet - that happens on hit)
		entry.lastAccess = c.currentCommit
	}
	return entry
}

// recordHit records a cache hit and updates frequency.
func (c *stateCache) recordHit(entry *cacheEntry[*ir.Node]) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Exponential moving average: favor recent accesses
	entry.frequency = entry.frequency*0.9 + 1.0
	entry.lastAccess = c.currentCommit
}

// setCompacted stores a compacted state in the cache.
func (c *stateCache) setCompacted(path string, node *ir.Node, endCommit int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	estimatedSize := estimateNodeSize(node)

	// Remove old entry if exists
	if old := c.compacted[path]; old != nil {
		c.totalSize -= old.estimatedSize
	}

	entry := &cacheEntry[*ir.Node]{
		Value:         node,
		EndCommit:     endCommit,
		estimatedSize: estimatedSize,
		frequency:     1.0, // Start with base frequency
		lastAccess:    c.currentCommit,
	}

	c.compacted[path] = entry
	c.totalSize += estimatedSize

	// Evict if over limits
	c.evictIfNeeded()
}

// setComputed stores a computed state in the cache.
func (c *stateCache) setComputed(path string, commit int64, node *ir.Node) {
	c.mu.Lock()
	defer c.mu.Unlock()

	estimatedSize := estimateNodeSize(node)

	// Don't cache if too large
	if estimatedSize > c.sizeThreshold*5 { // 5x threshold = too large
		return
	}

	key := cacheKey{path: path, commit: commit}

	// Remove old entry if exists
	if old := c.computed[key]; old != nil {
		c.totalSize -= old.estimatedSize
	}

	entry := &cacheEntry[*ir.Node]{
		Value:         node,
		EndCommit:     commit,
		estimatedSize: estimatedSize,
		frequency:     1.0, // Start with base frequency
		lastAccess:    c.currentCommit,
	}

	c.computed[key] = entry
	c.totalSize += estimatedSize

	// Evict if over limits
	c.evictIfNeeded()
}

// evictIfNeeded evicts entries when memory limits are exceeded.
func (c *stateCache) evictIfNeeded() {
	// Check soft limit
	if c.totalSize <= c.softLimit {
		return
	}

	// Collect all computed entries with heat scores
	type entryWithHeat struct {
		key   cacheKey
		entry *cacheEntry[*ir.Node]
		heat  float64
	}

	entries := make([]entryWithHeat, 0, len(c.computed))
	for key, entry := range c.computed {
		heat := entry.computeHeat(c.currentCommit, c.sizeThreshold)
		entries = append(entries, entryWithHeat{key: key, entry: entry, heat: heat})
	}

	// Sort by heat (lowest first = coldest)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].heat < entries[j].heat
	})

	// Evict coldest entries until under soft limit
	for _, ewh := range entries {
		if c.totalSize <= c.softLimit {
			break
		}
		c.totalSize -= ewh.entry.estimatedSize
		delete(c.computed, ewh.key)
	}

	// If still over hard limit, evict more aggressively
	if c.totalSize > c.hardLimit {
		// Evict all remaining computed entries below average heat
		if len(entries) > 0 {
			avgHeat := 0.0
			for _, ewh := range entries {
				if _, exists := c.computed[ewh.key]; exists {
					avgHeat += ewh.heat
				}
			}
			if len(c.computed) > 0 {
				avgHeat /= float64(len(c.computed))
			}

			for _, ewh := range entries {
				if c.totalSize <= c.hardLimit {
					break
				}
				if _, exists := c.computed[ewh.key]; exists && ewh.heat < avgHeat {
					c.totalSize -= ewh.entry.estimatedSize
					delete(c.computed, ewh.key)
				}
			}
		}
	}
}

// clearCompacted removes a compacted state from the cache.
func (c *stateCache) clearCompacted(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.compacted[path]; entry != nil {
		c.totalSize -= entry.estimatedSize
		delete(c.compacted, path)
	}
}

// clearComputed removes computed states for a path.
func (c *stateCache) clearComputed(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.computed {
		if key.path == path {
			c.totalSize -= entry.estimatedSize
			delete(c.computed, key)
		}
	}
}

// estimateNodeSize estimates memory size of an ir.Node tree.
// Since nodes are reconstructed (no shared subtrees), we can safely
// traverse and sum without worrying about double-counting.
func estimateNodeSize(node *ir.Node) int64 {
	if node == nil {
		return 0
	}

	// Base node structure: ~120 bytes
	// (Type, Parent, ParentIndex, ParentField, Tag, String, Bool, Number, Float64, Int64, Comment pointers)
	size := int64(120)

	// String data (actual bytes)
	size += int64(len(node.String))
	size += int64(len(node.Number))
	size += int64(len(node.Tag))

	// Lines (string data + slice overhead)
	for _, line := range node.Lines {
		size += int64(len(line))
	}
	size += int64(cap(node.Lines)) * 8 // slice header overhead

	// Fields and Values slices (pointers + capacity overhead)
	// Each pointer: 8 bytes, slice header: 24 bytes
	size += int64(len(node.Fields)) * 8
	size += int64(cap(node.Fields)) * 8
	size += int64(len(node.Values)) * 8
	size += int64(cap(node.Values)) * 8

	// Recursively estimate children
	for _, child := range node.Values {
		size += estimateNodeSize(child)
	}
	for _, field := range node.Fields {
		size += estimateNodeSize(field)
	}
	if node.Comment != nil {
		size += estimateNodeSize(node.Comment)
	}

	return size
}
