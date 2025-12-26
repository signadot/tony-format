package server

import (
	"strings"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// DefaultBroadcastTimeout is the default timeout for sending events to watchers.
// If a watcher doesn't read within this time, the watch is failed.
const DefaultBroadcastTimeout = 5 * time.Second

// WatchHub manages watches and broadcasts commit notifications to watchers.
// It is thread-safe and designed for concurrent access from multiple sessions.
type WatchHub struct {
	mu               sync.RWMutex
	watchers         map[string]map[*Watcher]struct{} // path -> set of watchers
	broadcastTimeout time.Duration                    // timeout for sending to watchers
}

// Watcher represents a watch on a path.
// The Events channel receives commit notifications that match the watched path.
// If the watcher can't keep up (Events channel blocks), the watch is failed
// and the Failed channel is closed.
type Watcher struct {
	Path       string                           // Watched path (prefix match)
	Scope      *string                          // Scope for COW isolation (nil = baseline only)
	Events     chan *storage.CommitNotification // Channel for receiving events
	Failed     chan struct{}                    // Closed when watch fails (slow consumer)
	FromCommit *int64                           // Starting commit for replay

	failOnce sync.Once // ensures Failed is closed only once
}

// NewWatchHub creates a new WatchHub instance with default timeout.
func NewWatchHub() *WatchHub {
	return &WatchHub{
		watchers:         make(map[string]map[*Watcher]struct{}),
		broadcastTimeout: DefaultBroadcastTimeout,
	}
}

// NewWatchHubWithTimeout creates a new WatchHub with a custom broadcast timeout.
func NewWatchHubWithTimeout(timeout time.Duration) *WatchHub {
	return &WatchHub{
		watchers:         make(map[string]map[*Watcher]struct{}),
		broadcastTimeout: timeout,
	}
}

// Watch adds a watcher for the given path.
// Events matching the path (by prefix) will be sent to the watcher's Events channel.
// Returns the watcher which can be used to unwatch later.
//
// The caller is responsible for:
// 1. Creating the Watcher with a buffered Events channel
// 2. Reading from the Events channel to avoid blocking
// 3. Calling Unwatch when done
func (h *WatchHub) Watch(watcher *Watcher) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.watchers[watcher.Path] == nil {
		h.watchers[watcher.Path] = make(map[*Watcher]struct{})
	}
	h.watchers[watcher.Path][watcher] = struct{}{}
}

// Unwatch removes a watcher.
// After unwatching, no more events will be sent to the watcher's channel.
// The caller is responsible for draining and closing the Events channel.
func (h *WatchHub) Unwatch(watcher *Watcher) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if watchers, ok := h.watchers[watcher.Path]; ok {
		delete(watchers, watcher)
		if len(watchers) == 0 {
			delete(h.watchers, watcher.Path)
		}
	}
}

// Broadcast sends a commit notification to all matching watchers.
// A watcher matches if any of the notification's KPaths has the watcher's path as a prefix.
//
// If a watcher's channel blocks for longer than the broadcast timeout, the watch
// is failed (Failed channel is closed) and the watcher is removed. This ensures slow
// consumers don't miss events silently - they are notified of failure instead.
//
// This method is designed to be used as a CommitNotifier callback:
//
//	storage.SetCommitNotifier(hub.Broadcast)
func (h *WatchHub) Broadcast(n *storage.CommitNotification) {
	h.mu.RLock()

	// Collect watchers to notify and potential failures
	type sendTarget struct {
		watcher     *Watcher
		watcherPath string
	}
	var targets []sendTarget

	for watcherPath, watchers := range h.watchers {
		if matchesPath(watcherPath, n.KPaths) {
			for watcher := range watchers {
				// Check scope filtering:
				// - Baseline watcher (scope=nil): only baseline events (n.ScopeID=nil)
				// - Scoped watcher: baseline events + matching scope events
				if !matchesScope(watcher.Scope, n.ScopeID) {
					continue
				}
				targets = append(targets, sendTarget{watcher: watcher, watcherPath: watcherPath})
			}
		}
	}
	h.mu.RUnlock()

	// Send to each target with timeout
	var failedWatchers []*Watcher
	for _, target := range targets {
		// Check if already failed
		select {
		case <-target.watcher.Failed:
			continue // Already failed, skip
		default:
		}

		// Try to send with timeout
		select {
		case target.watcher.Events <- n:
			// Sent successfully
		case <-time.After(h.broadcastTimeout):
			// Timeout - watcher is too slow, fail it
			target.watcher.failOnce.Do(func() {
				close(target.watcher.Failed)
			})
			failedWatchers = append(failedWatchers, target.watcher)
		case <-target.watcher.Failed:
			// Failed while waiting, skip
		}
	}

	// Remove failed watchers
	if len(failedWatchers) > 0 {
		h.mu.Lock()
		for _, watcher := range failedWatchers {
			if watchers, ok := h.watchers[watcher.Path]; ok {
				delete(watchers, watcher)
				if len(watchers) == 0 {
					delete(h.watchers, watcher.Path)
				}
			}
		}
		h.mu.Unlock()
	}
}

// GetCurrentCommit returns a function that retrieves the current commit.
// This is used by sessions to determine the replay range.
type CommitGetter func() (int64, error)

// WatcherCount returns the total number of active watchers.
// Useful for monitoring and debugging.
func (h *WatchHub) WatcherCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	count := 0
	for _, watchers := range h.watchers {
		count += len(watchers)
	}
	return count
}

// PathCount returns the number of unique paths being watched.
// Useful for monitoring and debugging.
func (h *WatchHub) PathCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.watchers)
}

// matchesPath checks if any of the kpaths matches the watch path.
// A match occurs when:
// - watchPath is empty (matches everything)
// - watchPath equals a kpath exactly
// - watchPath is a prefix of a kpath (kpath starts with watchPath + "." or watchPath + "[" or watchPath + "{")
// - a kpath is a prefix of watchPath (notification affects a parent of the watch)
func matchesPath(watchPath string, kpaths []string) bool {
	// Empty watch path matches everything
	if watchPath == "" {
		return true
	}

	for _, kp := range kpaths {
		// Exact match
		if kp == watchPath {
			return true
		}

		// kpath is under watchPath (watchPath is prefix)
		if strings.HasPrefix(kp, watchPath) {
			next := kp[len(watchPath):]
			if len(next) > 0 && (next[0] == '.' || next[0] == '[' || next[0] == '{') {
				return true
			}
		}

		// watchPath is under kpath (kpath is prefix) - notification affects parent
		if strings.HasPrefix(watchPath, kp) {
			next := watchPath[len(kp):]
			if len(next) > 0 && (next[0] == '.' || next[0] == '[' || next[0] == '{') {
				return true
			}
		}
	}

	return false
}

// matchesScope checks if a watcher should receive an event based on scope.
// - Baseline watcher (watcherScope=nil): only receive baseline events (eventScope=nil)
// - Scoped watcher: receive baseline events AND matching scope events
func matchesScope(watcherScope, eventScope *string) bool {
	if watcherScope == nil {
		// Baseline watcher: only baseline events
		return eventScope == nil
	}
	// Scoped watcher: baseline events + matching scope events
	if eventScope == nil {
		return true // Always include baseline events
	}
	return *watcherScope == *eventScope
}

// NewWatcher creates a new Watcher with a buffered events channel.
// bufferSize controls how many events can be buffered before the watch
// is failed due to slow consumption.
// scope is used for COW isolation: nil = baseline only, non-nil = baseline + scope events.
func NewWatcher(path string, scope *string, fromCommit *int64, bufferSize int) *Watcher {
	return &Watcher{
		Path:       path,
		Scope:      scope,
		FromCommit: fromCommit,
		Events:     make(chan *storage.CommitNotification, bufferSize),
		Failed:     make(chan struct{}),
	}
}

// IsFailed returns true if the watch has failed (slow consumer).
func (w *Watcher) IsFailed() bool {
	select {
	case <-w.Failed:
		return true
	default:
		return false
	}
}
