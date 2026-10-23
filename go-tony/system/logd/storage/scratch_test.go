package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// Scratch: a second document with no history (r482sz5rh12ksj0cvdn0). A write is a
// merge patch applied in memory; a read is the subtree now, or absent; the file is
// written behind the writes and reopens to the last state flushed.

func scratchNode(t *testing.T, text string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(text), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	return n
}

func scratchText(t *testing.T, n *ir.Node) string {
	t.Helper()
	if n == nil {
		return "<absent>"
	}
	var sb strings.Builder
	if err := encode.Encode(n, &sb, encode.EncodeComments(true), encode.EncodeBrackets(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return strings.TrimSpace(sb.String())
}

// scratchFileText is the scratch file's content, parsed and printed as scratchText
// prints a node, or "" for an empty file.
func scratchFileText(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, scratchFile))
	if err != nil {
		t.Fatalf("read scratch file: %v", err)
	}
	if len(data) == 0 {
		return ""
	}
	return scratchText(t, scratchNode(t, string(data)))
}

func scratchExpect(t *testing.T, s *Storage, path, want string) {
	t.Helper()
	got, err := s.ScratchRead(path)
	if err != nil {
		t.Fatalf("ScratchRead(%q): %v", path, err)
	}
	if want != "<absent>" {
		want = scratchText(t, scratchNode(t, want))
	}
	if text := scratchText(t, got); text != want {
		t.Fatalf("ScratchRead(%q) = %s, want %s", path, text, want)
	}
}

func TestScratch_WriteReadReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Absent before any write, at the root and below.
	scratchExpect(t, s, "", "<absent>")
	scratchExpect(t, s, "verse.iods.x.out", "<absent>")

	// A write at an absent path creates the containers above it; a sparse index too.
	if err := s.ScratchWrite("verse.iods.x.out", scratchNode(t, `{n: 1, note: "first"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.ScratchWrite("verse.iods.y.out", scratchNode(t, `{n: 2}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.ScratchWrite("slots{7}", scratchNode(t, `busy`)); err != nil {
		t.Fatalf("write sparse: %v", err)
	}
	scratchExpect(t, s, "verse.iods.x.out.n", "1")
	scratchExpect(t, s, "slots{7}", "busy")

	// Overwrite merges; a comment is kept; !delete removes a leaf.
	if err := s.ScratchWrite("verse.iods.x.out", scratchNode(t, "# second pass\n{n: 3, note: !delete}")); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	scratchExpect(t, s, "verse.iods.x.out", "# second pass\n{n: 3}")
	scratchExpect(t, s, "verse.iods.x.out.note", "<absent>")
	scratchExpect(t, s, "verse.iods.y.out.n", "2")

	// A read answers a copy: changing it does not change scratch.
	got, _ := s.ScratchRead("verse.iods.y.out")
	got.Values[0] = ir.FromInt(99)
	scratchExpect(t, s, "verse.iods.y.out.n", "2")

	// Close flushes; reopen reads what was written, comment included.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err = Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	scratchExpect(t, s, "verse.iods.x.out", "# second pass\n{n: 3}")
	scratchExpect(t, s, "slots{7}", "busy")

	// The root is a path: !delete there empties scratch, and the empty file reopens
	// as empty.
	if err := s.ScratchWrite("", scratchNode(t, `!delete`)); err != nil {
		t.Fatalf("delete root: %v", err)
	}
	scratchExpect(t, s, "", "<absent>")
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, scratchFile)); err != nil || len(data) != 0 {
		t.Fatalf("emptied scratch file = %q, %v; want empty", data, err)
	}
	s, err = Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen empty: %v", err)
	}
	scratchExpect(t, s, "", "<absent>")

	// A root write of null is stored as empty: scratch is never a bare null.
	if err := s.ScratchWrite("", scratchNode(t, `null`)); err != nil {
		t.Fatalf("write null root: %v", err)
	}
	scratchExpect(t, s, "", "<absent>")
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestScratch_Refusals(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.ScratchWrite("a.b", scratchNode(t, `{k: 1}`)); err != nil {
		t.Fatalf("write: %v", err)
	}

	// An operation which executes is refused as the document refuses it, and the
	// document is unchanged.
	err = s.ScratchWrite("a.c", scratchNode(t, `!pipe "echo hi"`))
	var unsafeOp *tx.UnsafeOpError
	if !errors.As(err, &unsafeOp) {
		t.Fatalf("!pipe: err = %v, want UnsafeOpError", err)
	}
	scratchExpect(t, s, "a", "{b: {k: 1}}")

	// A dense index into no array is refused by the apply; the next write succeeds
	// (the document is re-cloned whole after a failed apply).
	err = s.ScratchWrite("a.b.k[2]", scratchNode(t, `x`))
	var apply *ScratchApplyError
	if !errors.As(err, &apply) {
		t.Fatalf("dense index into a scalar: err = %v, want ScratchApplyError", err)
	}
	scratchExpect(t, s, "a", "{b: {k: 1}}")
	if err := s.ScratchWrite("a.d", scratchNode(t, `2`)); err != nil {
		t.Fatalf("write after a failed apply: %v", err)
	}
	scratchExpect(t, s, "a", "{b: {k: 1}, d: 2}")

	// A read that meets the wrong shape is a path error, not absent.
	_, err = s.ScratchRead("a.b.k.deeper")
	var pathErr *ScratchPathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("field under a scalar: err = %v, want ScratchPathError", err)
	}

	// A wildcard names a set, which scratch does not answer; nor does `..`.
	for _, p := range []string{"a.*", "a..k", "a[*]"} {
		if _, err := s.ScratchRead(p); err == nil {
			t.Fatalf("read %q: want an error", p)
		}
		if err := s.ScratchWrite(p, scratchNode(t, `1`)); err == nil {
			t.Fatalf("write %q: want an error", p)
		}
	}
	if err := s.ScratchWrite("a.e", nil); err == nil {
		t.Fatal("write with no data: want an error")
	}
}

func TestScratch_CorruptFileRefusesOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.Close()
	if err := os.WriteFile(filepath.Join(dir, scratchFile), []byte("{a: [1, 2"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = Open(dir, nil)
	if err == nil || !strings.Contains(err.Error(), scratchFile) {
		t.Fatalf("Open with a corrupt scratch file: err = %v, want one naming %s", err, scratchFile)
	}
}

// The file is a prefix of the write sequence: what a flush took is on disk, what came
// after it is not, and a failed flush is retried by the next tick.
func TestScratch_FlushOrderAndRetry(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.SetScratchFlush(time.Hour) // the ticker never fires in this test; flushes are explicit
	if got := s.ScratchFlush(); got != time.Hour {
		t.Fatalf("ScratchFlush = %v, want 1h", got)
	}
	write := func(path, text string) {
		t.Helper()
		if err := s.ScratchWrite(path, scratchNode(t, text)); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	write("a", "1")
	write("b", "2")
	if err := s.scratch.flushNow(false); err != nil {
		t.Fatalf("flushNow: %v", err)
	}
	write("c", "3")
	// Killed here, without Close: the file holds a and b, not c.
	if got, want := scratchFileText(t, dir), scratchText(t, scratchNode(t, "{a: 1, b: 2}")); got != want {
		t.Fatalf("file after a flush taken before c = %q, want {a: 1, b: 2}", got)
	}

	// A flush that cannot land leaves the document dirty, and the next one lands it.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	if err := s.scratch.flushNow(false); err == nil {
		os.Chmod(dir, 0755)
		t.Skip("the directory stayed writable (running as root?)")
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := s.scratch.flushNow(false); err != nil {
		t.Fatalf("flushNow after restore: %v", err)
	}
	if got, want := scratchFileText(t, dir), scratchText(t, scratchNode(t, "{a: 1, b: 2, c: 3}")); got != want {
		t.Fatalf("file after retry = %q, want {a: 1, b: 2, c: 3}", got)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// Writers, readers and flushes at once: no write is lost, every file written parses,
// and Close lands the last state. Run under -race.
func TestScratch_Concurrent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.SetScratchFlush(time.Millisecond)
	const n = 500
	var wg sync.WaitGroup
	for _, leaf := range []string{"x", "y"} {
		wg.Add(1)
		go func(leaf string) {
			defer wg.Done()
			for i := 1; i <= n; i++ {
				if err := s.ScratchWrite("counters."+leaf, ir.FromInt(int64(i))); err != nil {
					t.Errorf("write %s: %v", leaf, err)
					return
				}
			}
		}(leaf)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			if _, err := s.ScratchRead("counters"); err != nil {
				t.Errorf("read: %v", err)
				return
			}
			if err := s.scratch.flushNow(false); err != nil {
				t.Errorf("flushNow: %v", err)
				return
			}
			if data, err := os.ReadFile(filepath.Join(dir, scratchFile)); err == nil && len(data) > 0 {
				if _, err := parse.Parse(data); err != nil {
					t.Errorf("file does not parse: %v\n%s", err, data)
					return
				}
			}
		}
	}()
	wg.Wait()
	scratchExpect(t, s, "counters", "{x: 500, y: 500}")
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err = Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	scratchExpect(t, s, "counters", "{x: 500, y: 500}")
}
