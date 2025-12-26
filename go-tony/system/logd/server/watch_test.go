package server

import (
	"sync"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func TestWatchHub_SubscribeUnsubscribe(t *testing.T) {
	hub := NewWatchHub()

	// Initially empty
	if hub.WatcherCount() != 0 {
		t.Errorf("expected 0 watchers, got %d", hub.WatcherCount())
	}

	// Subscribe
	sub1 := NewWatcher("users", nil, nil, 10)
	hub.Watch(sub1)

	if hub.WatcherCount() != 1 {
		t.Errorf("expected 1 watcher, got %d", hub.WatcherCount())
	}
	if hub.PathCount() != 1 {
		t.Errorf("expected 1 path, got %d", hub.PathCount())
	}

	// Subscribe another to same path
	sub2 := NewWatcher("users", nil, nil, 10)
	hub.Watch(sub2)

	if hub.WatcherCount() != 2 {
		t.Errorf("expected 2 watchers, got %d", hub.WatcherCount())
	}
	if hub.PathCount() != 1 {
		t.Errorf("expected 1 path, got %d", hub.PathCount())
	}

	// Subscribe to different path
	sub3 := NewWatcher("posts", nil, nil, 10)
	hub.Watch(sub3)

	if hub.WatcherCount() != 3 {
		t.Errorf("expected 3 watchers, got %d", hub.WatcherCount())
	}
	if hub.PathCount() != 2 {
		t.Errorf("expected 2 paths, got %d", hub.PathCount())
	}

	// Unsubscribe one from users
	hub.Unwatch(sub1)

	if hub.WatcherCount() != 2 {
		t.Errorf("expected 2 watchers, got %d", hub.WatcherCount())
	}
	if hub.PathCount() != 2 {
		t.Errorf("expected 2 paths, got %d", hub.PathCount())
	}

	// Unsubscribe last from users - path should be removed
	hub.Unwatch(sub2)

	if hub.WatcherCount() != 1 {
		t.Errorf("expected 1 watcher, got %d", hub.WatcherCount())
	}
	if hub.PathCount() != 1 {
		t.Errorf("expected 1 path, got %d", hub.PathCount())
	}

	// Unsubscribe last
	hub.Unwatch(sub3)

	if hub.WatcherCount() != 0 {
		t.Errorf("expected 0 watchers, got %d", hub.WatcherCount())
	}
	if hub.PathCount() != 0 {
		t.Errorf("expected 0 paths, got %d", hub.PathCount())
	}
}

func TestWatchHub_Broadcast_ExactMatch(t *testing.T) {
	hub := NewWatchHub()

	sub := NewWatcher("users", nil, nil, 10)
	hub.Watch(sub)
	defer hub.Unwatch(sub)

	notification := &storage.CommitNotification{
		Commit:    1,
		TxSeq:     1,
		Timestamp: "2024-01-01T00:00:00Z",
		KPaths:    []string{"users"},
		Patch:     ir.FromString("test"),
	}

	hub.Broadcast(notification)

	select {
	case received := <-sub.Events:
		if received.Commit != 1 {
			t.Errorf("expected commit 1, got %d", received.Commit)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected to receive notification")
	}
}

func TestWatchHub_Broadcast_PrefixMatch(t *testing.T) {
	hub := NewWatchHub()

	// Subscribe to "users"
	sub := NewWatcher("users", nil, nil, 10)
	hub.Watch(sub)
	defer hub.Unwatch(sub)

	// Broadcast notification for "users.alice" (child of subscribed path)
	notification := &storage.CommitNotification{
		Commit:    1,
		TxSeq:     1,
		Timestamp: "2024-01-01T00:00:00Z",
		KPaths:    []string{"users.alice"},
		Patch:     ir.FromString("test"),
	}

	hub.Broadcast(notification)

	select {
	case received := <-sub.Events:
		if received.Commit != 1 {
			t.Errorf("expected commit 1, got %d", received.Commit)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected to receive notification for child path")
	}
}

func TestWatchHub_Broadcast_ParentMatch(t *testing.T) {
	hub := NewWatchHub()

	// Subscribe to "users.alice"
	sub := NewWatcher("users.alice", nil, nil, 10)
	hub.Watch(sub)
	defer hub.Unwatch(sub)

	// Broadcast notification for "users" (parent of subscribed path)
	notification := &storage.CommitNotification{
		Commit:    1,
		TxSeq:     1,
		Timestamp: "2024-01-01T00:00:00Z",
		KPaths:    []string{"users"},
		Patch:     ir.FromString("test"),
	}

	hub.Broadcast(notification)

	select {
	case received := <-sub.Events:
		if received.Commit != 1 {
			t.Errorf("expected commit 1, got %d", received.Commit)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected to receive notification for parent path")
	}
}

func TestWatchHub_Broadcast_NoMatch(t *testing.T) {
	hub := NewWatchHub()

	// Subscribe to "users"
	sub := NewWatcher("users", nil, nil, 10)
	hub.Watch(sub)
	defer hub.Unwatch(sub)

	// Broadcast notification for "posts" (different path)
	notification := &storage.CommitNotification{
		Commit:    1,
		TxSeq:     1,
		Timestamp: "2024-01-01T00:00:00Z",
		KPaths:    []string{"posts"},
		Patch:     ir.FromString("test"),
	}

	hub.Broadcast(notification)

	select {
	case <-sub.Events:
		t.Error("should not receive notification for unrelated path")
	case <-time.After(50 * time.Millisecond):
		// Expected - no notification
	}
}

func TestWatchHub_Broadcast_EmptyPath(t *testing.T) {
	hub := NewWatchHub()

	// Subscribe to "" (root - matches everything)
	sub := NewWatcher("", nil, nil, 10)
	hub.Watch(sub)
	defer hub.Unwatch(sub)

	// Broadcast notification for any path
	notification := &storage.CommitNotification{
		Commit:    1,
		TxSeq:     1,
		Timestamp: "2024-01-01T00:00:00Z",
		KPaths:    []string{"anything.here"},
		Patch:     ir.FromString("test"),
	}

	hub.Broadcast(notification)

	select {
	case received := <-sub.Events:
		if received.Commit != 1 {
			t.Errorf("expected commit 1, got %d", received.Commit)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected root subscription to receive all notifications")
	}
}

func TestWatchHub_Broadcast_MultipleKPaths(t *testing.T) {
	hub := NewWatchHub()

	// Subscribe to "users"
	sub := NewWatcher("users", nil, nil, 10)
	hub.Watch(sub)
	defer hub.Unwatch(sub)

	// Broadcast notification with multiple kpaths, one matching
	notification := &storage.CommitNotification{
		Commit:    1,
		TxSeq:     1,
		Timestamp: "2024-01-01T00:00:00Z",
		KPaths:    []string{"posts", "users.alice", "comments"},
		Patch:     ir.FromString("test"),
	}

	hub.Broadcast(notification)

	select {
	case received := <-sub.Events:
		if received.Commit != 1 {
			t.Errorf("expected commit 1, got %d", received.Commit)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected to receive notification when any kpath matches")
	}
}

func TestWatchHub_Broadcast_SlowConsumerFails(t *testing.T) {
	// Use short timeout for testing
	hub := NewWatchHubWithTimeout(50 * time.Millisecond)

	// Subscribe with small buffer
	sub := NewWatcher("users", nil, nil, 1)
	hub.Watch(sub)

	// Fill the buffer
	notification1 := &storage.CommitNotification{
		Commit: 1,
		KPaths: []string{"users"},
	}
	hub.Broadcast(notification1)

	// Verify first event was received
	select {
	case <-sub.Events:
		// Good
	default:
		t.Error("first event should be in buffer")
	}

	// Now buffer is empty but we won't read - next broadcast should timeout and fail
	notification2 := &storage.CommitNotification{
		Commit: 2,
		KPaths: []string{"users"},
	}

	// Don't read from sub.Events - simulate slow consumer
	// Refill buffer
	hub.Broadcast(notification2)

	// This broadcast should timeout and fail the subscription
	notification3 := &storage.CommitNotification{
		Commit: 3,
		KPaths: []string{"users"},
	}

	done := make(chan bool)
	go func() {
		hub.Broadcast(notification3)
		done <- true
	}()

	select {
	case <-done:
		// Broadcast completed (after timeout)
	case <-time.After(200 * time.Millisecond):
		t.Error("broadcast should complete after timeout")
	}

	// Subscription should be failed
	if !sub.IsFailed() {
		t.Error("subscription should be failed after timeout")
	}

	// Watcher should be removed from hub
	if hub.WatcherCount() != 0 {
		t.Errorf("expected 0 watchers after failure, got %d", hub.WatcherCount())
	}
}

func TestWatchHub_Broadcast_FastConsumerSucceeds(t *testing.T) {
	hub := NewWatchHubWithTimeout(50 * time.Millisecond)

	sub := NewWatcher("users", nil, nil, 10)
	hub.Watch(sub)
	defer hub.Unwatch(sub)

	// Send multiple notifications, reading each one
	for i := 0; i < 5; i++ {
		notification := &storage.CommitNotification{
			Commit: int64(i),
			KPaths: []string{"users"},
		}
		hub.Broadcast(notification)

		select {
		case received := <-sub.Events:
			if received.Commit != int64(i) {
				t.Errorf("expected commit %d, got %d", i, received.Commit)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("expected to receive notification")
		}
	}

	// Subscription should still be active
	if sub.IsFailed() {
		t.Error("subscription should not be failed")
	}
	if hub.WatcherCount() != 1 {
		t.Errorf("expected 1 watcher, got %d", hub.WatcherCount())
	}
}

func TestWatchHub_Concurrent(t *testing.T) {
	hub := NewWatchHub()

	var wg sync.WaitGroup
	const numGoroutines = 10
	const numOps = 100

	// Concurrent subscribe/unsubscribe
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				sub := NewWatcher("test", nil, nil, 10)
				hub.Watch(sub)
				hub.Unwatch(sub)
			}
		}(i)
	}

	// Concurrent broadcasts
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				hub.Broadcast(&storage.CommitNotification{
					Commit: int64(j),
					KPaths: []string{"test"},
				})
			}
		}(i)
	}

	wg.Wait()

	// Should end with no watchers
	if hub.WatcherCount() != 0 {
		t.Errorf("expected 0 watchers after concurrent ops, got %d", hub.WatcherCount())
	}
}

func TestMatchesPath(t *testing.T) {
	tests := []struct {
		name     string
		subPath  string
		kpaths   []string
		expected bool
	}{
		{"empty sub matches all", "", []string{"anything"}, true},
		{"exact match", "users", []string{"users"}, true},
		{"exact match with multiple", "users", []string{"posts", "users", "comments"}, true},
		{"child match dot", "users", []string{"users.alice"}, true},
		{"child match bracket", "users", []string{"users[0]"}, true},
		{"child match brace", "users", []string{"users{123}"}, true},
		{"parent match", "users.alice", []string{"users"}, true},
		{"no match different path", "users", []string{"posts"}, false},
		{"no match partial prefix", "user", []string{"users"}, false},
		{"no match suffix", "users", []string{"allusers"}, false},
		{"deep child match", "a", []string{"a.b.c.d"}, true},
		{"deep parent match", "a.b.c.d", []string{"a"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesPath(tt.subPath, tt.kpaths)
			if result != tt.expected {
				t.Errorf("matchesPath(%q, %v) = %v, want %v", tt.subPath, tt.kpaths, result, tt.expected)
			}
		})
	}
}
