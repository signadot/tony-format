package server

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// mockConn is a mock io.ReadWriteCloser for testing sessions.
// It blocks on Read when buffer is empty (like a real connection).
type mockConn struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	closed   bool
	mu       sync.Mutex
	cond     *sync.Cond
}

func newMockConn() *mockConn {
	c := &mockConn{
		readBuf:  &bytes.Buffer{},
		writeBuf: &bytes.Buffer{},
	}
	c.cond = sync.NewCond(&c.mu)
	return c
}

func (c *mockConn) Read(p []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Block until data is available or connection is closed
	for c.readBuf.Len() == 0 && !c.closed {
		c.cond.Wait()
	}

	if c.closed && c.readBuf.Len() == 0 {
		return 0, io.EOF
	}
	return c.readBuf.Read(p)
}

func (c *mockConn) Write(p []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, io.ErrClosedPipe
	}
	return c.writeBuf.Write(p)
}

func (c *mockConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.cond.Broadcast() // Wake up any blocked readers
	return nil
}

func (c *mockConn) WriteRequest(req string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readBuf.WriteString(req)
	c.readBuf.WriteString("\n\n") // Blank line as document separator
	c.cond.Signal() // Wake up reader
}

func (c *mockConn) GetResponses() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writeBuf.Bytes()
}

func TestSession_Hello(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	hub := NewWatchHub()
	conn := newMockConn()

	// Write hello request (bracketed format for wire protocol)
	conn.WriteRequest(`{hello: {clientId: "test-client"}}`)

	session := NewSession("test-server", conn, &SessionConfig{
		Storage: store,
		Hub:     hub,
	})

	// Run session in background
	done := make(chan error)
	go func() {
		done <- session.Run()
	}()

	// Wait a bit for processing
	time.Sleep(50 * time.Millisecond)

	// Close to end session
	conn.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not complete")
	}

	// Check response
	responses := conn.GetResponses()
	if len(responses) == 0 {
		t.Fatal("expected response")
	}

	// Parse response
	var resp api.SessionResponse
	if err := resp.FromTony(bytes.TrimSpace(responses)); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Result == nil || resp.Result.Hello == nil {
		t.Fatal("expected hello response")
	}
	if resp.Result.Hello.ServerID != "test-server" {
		t.Errorf("expected serverID 'test-server', got %q", resp.Result.Hello.ServerID)
	}
}

func TestSession_Match(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	// Write some data first
	tx, err := store.NewTx(1, nil)
	if err != nil {
		t.Fatalf("failed to create tx: %v", err)
	}

	patchData, _ := parse.Parse([]byte(`{users: {alice: {name: "Alice"}}}`))
	patch := &api.Patch{
		Patch: api.Body{Path: "", Data: patchData},
	}
	patcher, err := tx.NewPatcher(patch)
	if err != nil {
		t.Fatalf("failed to create patcher: %v", err)
	}
	result := patcher.Commit()
	if !result.Committed {
		t.Fatalf("failed to commit: %v", result.Error)
	}

	hub := NewWatchHub()
	conn := newMockConn()

	// Write match request (bracketed format for wire protocol)
	conn.WriteRequest(`{id: "req-1", match: {body: {path: users}}}`)

	session := NewSession("test-server", conn, &SessionConfig{
		Storage: store,
		Hub:     hub,
	})

	done := make(chan error)
	go func() {
		done <- session.Run()
	}()

	time.Sleep(50 * time.Millisecond)
	conn.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not complete")
	}

	responses := conn.GetResponses()
	t.Logf("Response: %s", responses)

	var resp api.SessionResponse
	if err := resp.FromTony(bytes.TrimSpace(responses)); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.ID == nil || *resp.ID != "req-1" {
		t.Errorf("expected id 'req-1', got %v", resp.ID)
	}
	if resp.Result == nil || resp.Result.Match == nil {
		t.Fatal("expected match result")
	}
	if resp.Result.Match.Commit != 1 {
		t.Errorf("expected commit 1, got %d", resp.Result.Match.Commit)
	}
}

func TestSession_MatchWithFilter(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	// Write some data with multiple items
	tx, err := store.NewTx(1, nil)
	if err != nil {
		t.Fatalf("failed to create tx: %v", err)
	}

	patchData, _ := parse.Parse([]byte(`{users: [{id: "1", name: "Alice", active: true}, {id: "2", name: "Bob", active: false}, {id: "3", name: "Charlie", active: true}]}`))
	patch := &api.Patch{
		Patch: api.Body{Path: "", Data: patchData},
	}
	patcher, err := tx.NewPatcher(patch)
	if err != nil {
		t.Fatalf("failed to create patcher: %v", err)
	}
	result := patcher.Commit()
	if !result.Committed {
		t.Fatalf("failed to commit: %v", result.Error)
	}

	hub := NewWatchHub()
	conn := newMockConn()

	// Write match request with filter (only active users)
	conn.WriteRequest(`{id: "req-1", match: {body: {path: users, data: {active: true}}}}`)

	session := NewSession("test-server", conn, &SessionConfig{
		Storage: store,
		Hub:     hub,
	})

	done := make(chan error)
	go func() {
		done <- session.Run()
	}()

	time.Sleep(50 * time.Millisecond)
	conn.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not complete")
	}

	responses := conn.GetResponses()
	t.Logf("Response: %s", responses)

	var resp api.SessionResponse
	if err := resp.FromTony(bytes.TrimSpace(responses)); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Result == nil || resp.Result.Match == nil {
		t.Fatal("expected match result")
	}

	// Should have filtered to only active users (Alice and Charlie)
	body := resp.Result.Match.Body
	if body == nil {
		t.Fatal("expected body in match result")
	}
	if body.Type != ir.ArrayType {
		t.Fatalf("expected array, got %v", body.Type)
	}
	if len(body.Values) != 2 {
		t.Errorf("expected 2 active users, got %d", len(body.Values))
	}
}

func TestSession_Patch(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	hub := NewWatchHub()
	conn := newMockConn()

	// Write patch request (bracketed format for wire protocol)
	conn.WriteRequest(`{id: "patch-1", patch: {patch: {path: "", data: {users: {bob: {name: "Bob"}}}}}}`)

	commitCalled := false
	session := NewSession("test-server", conn, &SessionConfig{
		Storage:  store,
		Hub:      hub,
		OnCommit: func() { commitCalled = true },
	})

	done := make(chan error)
	go func() {
		done <- session.Run()
	}()

	time.Sleep(50 * time.Millisecond)
	conn.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not complete")
	}

	responses := conn.GetResponses()
	t.Logf("Response: %s", responses)

	var resp api.SessionResponse
	if err := resp.FromTony(bytes.TrimSpace(responses)); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.ID == nil || *resp.ID != "patch-1" {
		t.Errorf("expected id 'patch-1', got %v", resp.ID)
	}
	if resp.Result == nil || resp.Result.Patch == nil {
		t.Fatal("expected patch result")
	}
	if resp.Result.Patch.Commit != 1 {
		t.Errorf("expected commit 1, got %d", resp.Result.Patch.Commit)
	}
	if !commitCalled {
		t.Error("expected onCommit to be called")
	}
}

func TestSession_SubscribeUnsubscribe(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	hub := NewWatchHub()
	conn := newMockConn()

	// Write subscribe request (bracketed format for wire protocol)
	conn.WriteRequest(`{id: "sub-1", watch: {path: users, fullState: false}}`)

	session := NewSession("test-server", conn, &SessionConfig{
		Storage: store,
		Hub:     hub,
	})

	done := make(chan error)
	go func() {
		done <- session.Run()
	}()

	time.Sleep(50 * time.Millisecond)

	// Check hub has watcher
	if hub.WatcherCount() != 1 {
		t.Errorf("expected 1 watcher, got %d", hub.WatcherCount())
	}

	// Send unsubscribe (bracketed format for wire protocol)
	conn.WriteRequest(`{unwatch: {path: users}}`)

	time.Sleep(50 * time.Millisecond)

	// Check hub has no watcher
	if hub.WatcherCount() != 0 {
		t.Errorf("expected 0 watchers, got %d", hub.WatcherCount())
	}

	conn.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not complete")
	}

	responses := conn.GetResponses()
	t.Logf("Responses: %s", responses)

	// Should have subscribe result and unsubscribe result
	// Split on blank lines (double newline) to get individual documents
	docs := splitTonyDocs(responses)
	var foundSubscribe, foundUnsubscribe bool
	for _, doc := range docs {
		var resp api.SessionResponse
		if err := resp.FromTony(doc); err != nil {
			t.Logf("failed to parse doc: %v", err)
			continue
		}
		if resp.Result != nil {
			if resp.Result.Watch != nil {
				foundSubscribe = true
				if resp.Result.Watch.Watching != "users" {
					t.Errorf("expected subscribed 'users', got %q", resp.Result.Watch.Watching)
				}
			}
			if resp.Result.Unwatch != nil {
				foundUnsubscribe = true
				if resp.Result.Unwatch.Unwatched != "users" {
					t.Errorf("expected unsubscribed 'users', got %q", resp.Result.Unwatch.Unwatched)
				}
			}
		}
	}
	if !foundSubscribe {
		t.Error("expected subscribe result")
	}
	if !foundUnsubscribe {
		t.Error("expected unsubscribe result")
	}
}

// splitTonyDocs splits response bytes into individual Tony documents.
// Documents are separated by blank lines.
func splitTonyDocs(data []byte) [][]byte {
	var docs [][]byte
	// Split on double newline (blank line separator)
	parts := bytes.Split(data, []byte("\n\n"))
	for _, part := range parts {
		part = bytes.TrimSpace(part)
		if len(part) > 0 {
			docs = append(docs, part)
		}
	}
	return docs
}

func TestSession_SubscribeReceivesEvents(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	hub := NewWatchHub()

	// Connect hub to storage notifications
	store.SetCommitNotifier(hub.Broadcast)

	conn := newMockConn()

	// Write subscribe request (bracketed format for wire protocol)
	conn.WriteRequest(`{watch: {path: users, fullState: false}}`)

	session := NewSession("test-server", conn, &SessionConfig{
		Storage: store,
		Hub:     hub,
	})

	done := make(chan error)
	go func() {
		done <- session.Run()
	}()

	time.Sleep(50 * time.Millisecond)

	// Now commit something that matches the subscription
	tx, err := store.NewTx(1, nil)
	if err != nil {
		t.Fatalf("failed to create tx: %v", err)
	}

	patchData, _ := parse.Parse([]byte(`{users: {alice: {name: "Alice"}}}`))
	patch := &api.Patch{
		Patch: api.Body{Path: "", Data: patchData},
	}
	patcher, err := tx.NewPatcher(patch)
	if err != nil {
		t.Fatalf("failed to create patcher: %v", err)
	}
	result := patcher.Commit()
	if !result.Committed {
		t.Fatalf("failed to commit: %v", result.Error)
	}

	// Wait for event to be forwarded
	time.Sleep(100 * time.Millisecond)

	conn.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not complete")
	}

	responses := conn.GetResponses()
	t.Logf("Responses: %s", responses)

	// Should have subscribe result and patch event
	docs := splitTonyDocs(responses)
	var foundEvent bool
	for _, doc := range docs {
		var resp api.SessionResponse
		if err := resp.FromTony(doc); err != nil {
			continue
		}
		if resp.Event != nil && resp.Event.Patch != nil {
			foundEvent = true
			if resp.Event.Commit != 1 {
				t.Errorf("expected event commit 1, got %d", resp.Event.Commit)
			}
		}
	}
	if !foundEvent {
		t.Error("expected to receive patch event from subscription")
	}
}

func TestSession_SubscribeWithReplay(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	hub := NewWatchHub()

	// Connect hub to storage notifications
	store.SetCommitNotifier(hub.Broadcast)

	// Create some initial commits
	for i := 1; i <= 3; i++ {
		tx, err := store.NewTx(1, nil)
		if err != nil {
			t.Fatalf("failed to create tx: %v", err)
		}
		patchData, _ := parse.Parse([]byte(fmt.Sprintf(`{users: {user%d: {name: "User %d"}}}`, i, i)))
		patch := &api.Patch{
			Patch: api.Body{Path: "", Data: patchData},
		}
		patcher, err := tx.NewPatcher(patch)
		if err != nil {
			t.Fatalf("failed to create patcher: %v", err)
		}
		result := patcher.Commit()
		if !result.Committed {
			t.Fatalf("failed to commit: %v", result.Error)
		}
	}

	conn := newMockConn()

	// Subscribe with fromCommit=0 to replay from beginning
	conn.WriteRequest(`{watch: {path: users, fromCommit: 0, fullState: false}}`)

	session := NewSession("test-server", conn, &SessionConfig{
		Storage: store,
		Hub:     hub,
	})

	done := make(chan error)
	go func() {
		done <- session.Run()
	}()

	// Wait for replay to complete
	time.Sleep(100 * time.Millisecond)

	// Now add another commit (live event)
	tx, err := store.NewTx(1, nil)
	if err != nil {
		t.Fatalf("failed to create tx: %v", err)
	}
	patchData, _ := parse.Parse([]byte(`{users: {user4: {name: "User 4"}}}`))
	patch := &api.Patch{
		Patch: api.Body{Path: "", Data: patchData},
	}
	patcher, err := tx.NewPatcher(patch)
	if err != nil {
		t.Fatalf("failed to create patcher: %v", err)
	}
	result := patcher.Commit()
	if !result.Committed {
		t.Fatalf("failed to commit: %v", result.Error)
	}

	// Wait for live event
	time.Sleep(100 * time.Millisecond)

	conn.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not complete")
	}

	responses := conn.GetResponses()
	t.Logf("Responses: %s", responses)

	// Parse all documents
	docs := splitTonyDocs(responses)

	var (
		replayedCommits   []int64
		foundReplayComplete bool
		liveCommits       []int64
	)

	inReplay := true
	for _, doc := range docs {
		var resp api.SessionResponse
		if err := resp.FromTony(doc); err != nil {
			continue
		}

		// Check for replay complete
		if resp.Event != nil && resp.Event.ReplayComplete {
			foundReplayComplete = true
			inReplay = false
			continue
		}

		// Track patch events
		if resp.Event != nil && resp.Event.Patch != nil {
			if inReplay {
				replayedCommits = append(replayedCommits, resp.Event.Commit)
			} else {
				liveCommits = append(liveCommits, resp.Event.Commit)
			}
		}
	}

	// Should have received 3 replayed patches (commits 1, 2, 3)
	if len(replayedCommits) != 3 {
		t.Errorf("expected 3 replayed commits, got %d: %v", len(replayedCommits), replayedCommits)
	}

	if !foundReplayComplete {
		t.Error("expected replay complete event")
	}

	// Should have received 1 live event (commit 4)
	if len(liveCommits) != 1 {
		t.Errorf("expected 1 live commit, got %d: %v", len(liveCommits), liveCommits)
	}
	if len(liveCommits) > 0 && liveCommits[0] != 4 {
		t.Errorf("expected live commit 4, got %d", liveCommits[0])
	}
}

func TestSession_SubscribeWithFullStateReplay(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	hub := NewWatchHub()

	// Create initial state
	tx, err := store.NewTx(1, nil)
	if err != nil {
		t.Fatalf("failed to create tx: %v", err)
	}
	patchData, _ := parse.Parse([]byte(`{users: {alice: {name: "Alice"}}}`))
	patch := &api.Patch{
		Patch: api.Body{Path: "", Data: patchData},
	}
	patcher, err := tx.NewPatcher(patch)
	if err != nil {
		t.Fatalf("failed to create patcher: %v", err)
	}
	result := patcher.Commit()
	if !result.Committed {
		t.Fatalf("failed to commit: %v", result.Error)
	}

	// Add another commit
	tx, err = store.NewTx(1, nil)
	if err != nil {
		t.Fatalf("failed to create tx: %v", err)
	}
	patchData, _ = parse.Parse([]byte(`{users: {bob: {name: "Bob"}}}`))
	patch = &api.Patch{
		Patch: api.Body{Path: "", Data: patchData},
	}
	patcher, err = tx.NewPatcher(patch)
	if err != nil {
		t.Fatalf("failed to create patcher: %v", err)
	}
	result = patcher.Commit()
	if !result.Committed {
		t.Fatalf("failed to commit: %v", result.Error)
	}

	conn := newMockConn()

	// Subscribe with fromCommit=1 and fullState=true
	// Should get state at commit 1, then patch for commit 2
	conn.WriteRequest(`{watch: {path: users, fromCommit: 1, fullState: true}}`)

	session := NewSession("test-server", conn, &SessionConfig{
		Storage: store,
		Hub:     hub,
	})

	done := make(chan error)
	go func() {
		done <- session.Run()
	}()

	time.Sleep(100 * time.Millisecond)
	conn.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not complete")
	}

	responses := conn.GetResponses()
	t.Logf("Responses: %s", responses)

	docs := splitTonyDocs(responses)

	var (
		foundState          bool
		stateCommit         int64
		foundReplayComplete bool
		replayedCommits     []int64
	)

	for _, doc := range docs {
		var resp api.SessionResponse
		if err := resp.FromTony(doc); err != nil {
			continue
		}

		if resp.Event != nil {
			if resp.Event.State != nil {
				foundState = true
				stateCommit = resp.Event.Commit
			}
			if resp.Event.Patch != nil {
				replayedCommits = append(replayedCommits, resp.Event.Commit)
			}
			if resp.Event.ReplayComplete {
				foundReplayComplete = true
			}
		}
	}

	if !foundState {
		t.Error("expected state event")
	}
	if stateCommit != 1 {
		t.Errorf("expected state at commit 1, got %d", stateCommit)
	}

	// Should have replayed commit 2
	if len(replayedCommits) != 1 {
		t.Errorf("expected 1 replayed commit, got %d: %v", len(replayedCommits), replayedCommits)
	}
	if len(replayedCommits) > 0 && replayedCommits[0] != 2 {
		t.Errorf("expected replayed commit 2, got %d", replayedCommits[0])
	}

	if !foundReplayComplete {
		t.Error("expected replay complete event")
	}
}

func TestSession_MultiParticipantTransaction(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	hub := NewWatchHub()

	// Create session for newtx request
	conn1 := newMockConn()
	conn1.WriteRequest(`{newtx: {participants: 2}}`)

	session1 := NewSession("test-server-1", conn1, &SessionConfig{
		Storage: store,
		Hub:     hub,
	})

	done1 := make(chan error)
	go func() {
		done1 <- session1.Run()
	}()

	time.Sleep(50 * time.Millisecond)

	// Get the txId from response
	responses := conn1.GetResponses()
	t.Logf("NewTx response: %s", responses)
	var resp api.SessionResponse
	if err := resp.FromTony(bytes.TrimSpace(responses)); err != nil {
		t.Fatalf("failed to parse newtx response: %v (raw: %s)", err, responses)
	}

	if resp.Result == nil || resp.Result.NewTx == nil {
		t.Fatalf("expected newtx response, got: %+v", resp)
	}
	txID := resp.Result.NewTx.TxID
	t.Logf("Created transaction with ID: %d", txID)

	// Now submit 2 patches concurrently from different sessions
	var wg sync.WaitGroup
	results := make([]int64, 2)
	errors := make([]error, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			conn := newMockConn()
			path := fmt.Sprintf("data%d", idx)
			req := fmt.Sprintf(`{id: "p%d", patch: {txId: %d, patch: {path: %s, data: {value: %d}}}}`, idx, txID, path, idx)
			conn.WriteRequest(req)

			session := NewSession(fmt.Sprintf("patch-session-%d", idx), conn, &SessionConfig{
				Storage: store,
				Hub:     hub,
			})

			done := make(chan error)
			go func() {
				done <- session.Run()
			}()

			// Wait for response (Commit blocks until all participants join)
			time.Sleep(200 * time.Millisecond)
			conn.Close()

			<-done

			responses := conn.GetResponses()
			var resp api.SessionResponse
			if err := resp.FromTony(bytes.TrimSpace(responses)); err != nil {
				errors[idx] = fmt.Errorf("failed to parse response: %v", err)
				return
			}

			if resp.Error != nil {
				errors[idx] = fmt.Errorf("error response: %s: %s", resp.Error.Code, resp.Error.Message)
				return
			}

			if resp.Result == nil || resp.Result.Patch == nil {
				errors[idx] = fmt.Errorf("expected patch response")
				return
			}

			results[idx] = resp.Result.Patch.Commit
		}(i)
	}

	wg.Wait()

	// Clean up first session
	conn1.Close()
	<-done1

	// Check results
	for i, err := range errors {
		if err != nil {
			t.Errorf("participant %d error: %v", i, err)
		}
	}

	// Both participants should get the same commit number
	if results[0] != results[1] {
		t.Errorf("expected same commit for all participants, got %d and %d", results[0], results[1])
	}
	if results[0] == 0 {
		t.Error("expected non-zero commit number")
	}
	t.Logf("Both participants received commit: %d", results[0])
}

