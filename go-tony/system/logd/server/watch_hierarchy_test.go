package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func TestHierarchicalWatch(t *testing.T) {
	// Setup server
	tmpDir, err := os.MkdirTemp("", "logd-watch-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := storage.Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(s)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Channel to receive updates
	updates := make(chan *ir.Node, 10)
	errChan := make(chan error, 1)

	// Start watch in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		client := &http.Client{}
		req, err := http.NewRequestWithContext(ctx, "WATCH", ts.URL+"/api/data", bytes.NewBufferString(`{"path": "/root"}`))
		if err != nil {
			errChan <- err
			return
		}
		req.Header.Set("Content-Type", "application/x-tony")

		resp, err := client.Do(req)
		if err != nil {
			// If context canceled, ignore error
			if ctx.Err() == nil {
				errChan <- err
			}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errChan <- fmt.Errorf("unexpected status: %d, body: %s", resp.StatusCode, body)
			return
		}

		// Read stream
		readBuf := make([]byte, 1024)
		var accumulator []byte

		for {
			n, err := resp.Body.Read(readBuf)
			if n > 0 {
				accumulator = append(accumulator, readBuf[:n]...)

				// Split by separator
				// We use a regex-like split manually
				parts := bytes.Split(accumulator, []byte("---\n"))

				var newAccumulator []byte

				for i, part := range parts {
					part = bytes.TrimSpace(part)
					if len(part) == 0 {
						continue
					}

					// Try to parse
					doc, parseErr := parse.Parse(part)
					if parseErr == nil {
						// Successfully parsed a doc
						updates <- doc
					} else {
						// Failed to parse.
						// If this is the last part, it might be incomplete.
						// If it's not the last part, it's a malformed doc (or we split wrong).
						if i == len(parts)-1 {
							// Keep for next read, but we need to restore the separator if we split it?
							// Actually, bytes.Split removes separator.
							// If we have [Doc1, IncompleteDoc2], we processed Doc1.
							// We want newAccumulator to be IncompleteDoc2.
							// But wait, if we split by ---\n, we lost the separator.
							// That's fine, parse doesn't need it.
							newAccumulator = part
						} else {
							// Middle part failed to parse. Log it?
							// For now, ignore or treat as incomplete?
							// It shouldn't happen in this test if stream is clean.
						}
					}
				}
				accumulator = newAccumulator
			}
			if err != nil {
				if err != io.EOF && ctx.Err() == nil {
					errChan <- err
				}
				return
			}
		}
	}()

	// Wait for watch to establish
	time.Sleep(100 * time.Millisecond)

	timestamp := time.Now().UTC().Format(time.RFC3339)

	// 1. Write to /root/child (Object)
	t.Log("Writing to /root/child...")
	childPatch, _ := parse.Parse([]byte(`val: 1`))
	_, _, err = s.WriteDiffAtomically("/root/child", timestamp, childPatch, false)
	if err != nil {
		t.Fatal(err)
	}

	// Verify update 1
	select {
	case doc := <-updates:
		t.Logf("Received Update 1: %v", doc)
		diff := ir.Get(doc, "diff")
		child := ir.Get(diff, "child")
		if child == nil {
			t.Errorf("Update 1: expected 'child' field, got %v", diff)
		}
		val := ir.Get(child, "val")
		if val == nil {
			t.Errorf("Update 1: expected child.val=1, got nil")
		} else {
			// Check Int64 since Number string might be empty
			if val.Int64 == nil || *val.Int64 != 1 {
				t.Errorf("Update 1: expected child.val=1, got %v", val)
			}
		}
	case err := <-errChan:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for update 1")
	}

	// 2. Write to /root/sparse (IntKeyed Map)
	t.Log("Writing to /root/sparse...")
	sparsePatch, _ := parse.Parse([]byte(`1: "a"`))
	// Add !sparsearray tag to the diff
	sparsePatch.Tag = "!sparsearray"

	_, _, err = s.WriteDiffAtomically("/root/sparse", timestamp, sparsePatch, false)
	if err != nil {
		t.Fatal(err)
	}

	// Verify update 2
	select {
	case doc := <-updates:
		t.Logf("Received Update 2: %v", doc)
		diff := ir.Get(doc, "diff")

		// Check sparse array structure
		sparse := ir.Get(diff, "sparse")
		if sparse == nil {
			t.Errorf("Update 2: expected 'sparse' field, got %v", diff)
		} else {
			// Verify it has !sparsearray tag
			if !strings.Contains(sparse.Tag, "!sparsearray") {
				t.Errorf("Update 2: expected !sparsearray tag, got %q", sparse.Tag)
			}

			// Check for int key 1
			// In sparse arrays, keys are NumberType nodes in Fields
			found := false
			for i, f := range sparse.Fields {
				// Check Int64 value of the key
				if f.Int64 != nil && *f.Int64 == 1 {
					found = true
					// Check value
					val := sparse.Values[i]
					if val.String != "a" {
						t.Errorf("Update 2: expected key 1 to have value 'a', got %v", val)
					}
					break
				}
			}
			if !found {
				t.Errorf("Update 2: key 1 not found in sparse array: %v", sparse)
			}
		}

		// Check that 'child' from previous update is NOT expected to be here (it's a diff)
		child := ir.Get(diff, "child")
		if child != nil {
			t.Logf("Update 2: 'child' field present (unexpected for diff stream but allowed if unchanged?)")
		}

	case err := <-errChan:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for update 2")
	}

	// 3. Write to /root/sparse/123 (Child of Sparse)
	// This is the hierarchical sparse array write
	t.Log("Writing to /root/sparse/123...")
	itemPatch, _ := parse.Parse([]byte(`foo: "bar"`))
	_, _, err = s.WriteDiffAtomically("/root/sparse/123", timestamp, itemPatch, false)
	if err != nil {
		t.Fatal(err)
	}

	// Verify update 3
	select {
	case doc := <-updates:
		t.Logf("Received Update 3: %v", doc)
		diff := ir.Get(doc, "diff")

		// Check sparse array structure
		sparse := ir.Get(diff, "sparse")
		if sparse == nil {
			t.Errorf("Update 3: expected 'sparse' field, got %v", diff)
		} else {
			// Verify it has !sparsearray tag
			if !strings.Contains(sparse.Tag, "!sparsearray") {
				t.Errorf("Update 3: expected !sparsearray tag, got %q", sparse.Tag)
			}

			// Check for int key 123
			found := false
			for i, f := range sparse.Fields {
				if f.Int64 != nil && *f.Int64 == 123 {
					found = true
					// Check value: { "foo": "bar" }
					val := sparse.Values[i]
					foo := ir.Get(val, "foo")
					if foo == nil || foo.String != "bar" {
						t.Errorf("Update 3: expected key 123 to have value {foo: bar}, got %v", val)
					}
					break
				}
			}
			if !found {
				t.Errorf("Update 3: key 123 not found in sparse array: %v", sparse)
			}
		}

	case err := <-errChan:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for update 3")
	}
}

func TestHierarchicalWatch_TypeChangeConflict(t *testing.T) {
	dir, err := os.MkdirTemp("", "logd-test-conflict")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	s, err := storage.Open(dir, 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(&Server{storage: s})
	defer ts.Close()

	// 1. Write Sparse Array at /root/conflict AND a child in the same commit
	// We manually write diffs to simulate a transaction with multiple diffs at the same commit count.
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)

	// Create a sparse array diff using ir.FromIntKeysMap (which sets the !sparsearray tag)
	sparsePatch := ir.FromIntKeysMap(map[uint32]*ir.Node{
		1: ir.FromString("one"),
	})

	// Commit 1, TxSeq 1
	if err := s.WriteDiff("/root/conflict", 1, 1, timestamp, sparsePatch, false); err != nil {
		t.Fatal(err)
	}
	// Manually write metadata for sparse array
	if err := s.FS.WritePathMetadata("/root/conflict", &storage.PathMetadata{IsSparseArray: true}); err != nil {
		t.Fatal(err)
	}

	childPatch, _ := parse.Parse([]byte(`"two"`))
	// Same commit count and txSeq for child
	if err := s.WriteDiff("/root/conflict/2", 1, 1, timestamp, childPatch, false); err != nil {
		t.Fatal(err)
	}

	// Now /root/conflict has a child "2".
	// Metadata for /root/conflict is Sparse (from step 1).

	// 2. Change /root/conflict to Object
	// This updates metadata to Object (IsSparseArray=false)
	objectPatch, _ := parse.Parse([]byte(`{ "foo": "bar" }`))

	// Commit 2, TxSeq 2
	if err := s.WriteDiff("/root/conflict", 2, 2, timestamp, objectPatch, false); err != nil {
		t.Fatal(err)
	}
	// Update metadata
	if err := s.FS.WritePathMetadata("/root/conflict", &storage.PathMetadata{IsSparseArray: false}); err != nil {
		t.Fatal(err)
	}

	// 3. Watch /root/conflict from beginning
	// We expect an error when processing the first commit (Sparse) because metadata is now Object.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, "WATCH", ts.URL+"/api/data", bytes.NewBufferString(`{"path": "/root/conflict"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-tony")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	// Read stream
	buf := make([]byte, 1024)

	// Continue reading until EOF
	// We expect EOF (stream closed) without receiving all data, or maybe just EOF.
	// If we receive the first update successfully, then the test failed (it should have errored).
	// But wait, the first update is the one that fails.
	// So we should receive NOTHING (or partial garbage) and then EOF.

	receivedDoc := false
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			t.Logf("Read %d bytes", n)
			// Try to parse
			parts := bytes.Split(buf[:n], []byte("---\n"))
			for _, part := range parts {
				part = bytes.TrimSpace(part)
				if len(part) > 0 {
					if _, err := parse.Parse(part); err == nil {
						receivedDoc = true
						t.Logf("Received unexpected doc: %s", part)
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				t.Log("Received EOF")
				break
			}
			t.Logf("Read error: %v", err)
			t.Fatal(err)
		}
	}

	if receivedDoc {
		t.Error("Expected stream to fail before sending document, but received document")
	}
}
