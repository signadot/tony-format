package dlog

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

func testEntry(commit int64, val string) *Entry {
	return &Entry{
		Commit:    commit,
		Timestamp: time.Now().Format(time.RFC3339),
		Patch:     ir.FromMap(map[string]*ir.Node{"k": ir.FromString(val)}),
	}
}

// Appends must land at the position they report, even after the file handle has been
// reopened. swapLogFile and reopenLogFile install a fresh handle (offset 0) and set
// position to the file size without seeking; when appends used the shared file offset,
// the next entry was written at offset 0 — overwriting the head of the log — while the
// index recorded position = size. Both entries then read back as garbage.
func TestAppendAfterReopen_LandsAtReportedPosition(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	pos1, lf, err := dl.AppendEntry(testEntry(1, "first"))
	if err != nil {
		t.Fatalf("AppendEntry(1) error = %v", err)
	}

	// Exactly what swapLogFile/reopenLogFile do to the active handle.
	logFile := dl.logA
	logFile.mu.Lock()
	if err := logFile.file.Close(); err != nil {
		logFile.mu.Unlock()
		t.Fatalf("close: %v", err)
	}
	err = dl.reopenLogFile(logFile)
	logFile.mu.Unlock()
	if err != nil {
		t.Fatalf("reopenLogFile() error = %v", err)
	}

	pos2, _, err := dl.AppendEntry(testEntry(2, "second"))
	if err != nil {
		t.Fatalf("AppendEntry(2) error = %v", err)
	}
	if pos2 <= pos1 {
		t.Fatalf("second entry position %d does not follow first at %d", pos2, pos1)
	}

	got1, err := dl.ReadEntryAt(lf, pos1, 0)
	if err != nil {
		t.Fatalf("ReadEntryAt(pos1=%d) error = %v", pos1, err)
	}
	if got1.Commit != 1 {
		t.Errorf("entry at pos1: commit = %d, want 1", got1.Commit)
	}

	got2, err := dl.ReadEntryAt(lf, pos2, 0)
	if err != nil {
		t.Fatalf("ReadEntryAt(pos2=%d) error = %v", pos2, err)
	}
	if got2.Commit != 2 {
		t.Errorf("entry at pos2: commit = %d, want 2", got2.Commit)
	}
}

// A failed append must not move the write frontier. If position is left behind the real
// end of file, every subsequent entry reports a position that does not point at its own
// header, and the whole log after that point is unreadable by index lookup.
func TestAppendPositionMatchesFileSize(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	for i := int64(1); i <= 5; i++ {
		pos, _, err := dl.AppendEntry(testEntry(i, "v"))
		if err != nil {
			t.Fatalf("AppendEntry(%d) error = %v", i, err)
		}
		size, err := dl.logA.Size()
		if err != nil {
			t.Fatalf("Size() error = %v", err)
		}
		if got := dl.logA.Position(); got != size {
			t.Fatalf("after entry %d at pos %d: position = %d, file size = %d", i, pos, got, size)
		}
	}
}

// A torn tail — a length prefix whose payload never made it to disk — must be detected on
// open and excluded from the append point, not silently adopted. Adopting it puts every
// later frame boundary at the wrong offset. The bytes stay on disk and are overwritten by
// the next append; nothing is deleted.
func TestOpenExcludesTornTailFromAppendPoint(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}

	pos1, lf, err := dl.AppendEntry(testEntry(1, "keep"))
	if err != nil {
		t.Fatalf("AppendEntry(1) error = %v", err)
	}
	goodEnd := dl.logA.Position()
	if err := dl.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Simulate a crash between the header write and the payload write: a 4-byte length
	// prefix claiming 4096 bytes, with nothing after it.
	path := filepath.Join(tmpDir, "logA")
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr, 4096)
	if _, err := f.WriteAt(hdr, goodEnd); err != nil {
		t.Fatalf("write torn header: %v", err)
	}
	f.Close()

	dl2, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() (reopen) error = %v", err)
	}
	defer dl2.Close()

	if got := dl2.logA.Position(); got != goodEnd {
		t.Errorf("position after reopen = %d, want %d (torn tail not excluded)", got, goodEnd)
	}

	// The surviving entry must still read, and a new append must be readable too.
	if _, err := dl2.ReadEntryAt(lf, pos1, 0); err != nil {
		t.Fatalf("ReadEntryAt(pos1) after reopen error = %v", err)
	}
	pos2, _, err := dl2.AppendEntry(testEntry(2, "after"))
	if err != nil {
		t.Fatalf("AppendEntry(2) error = %v", err)
	}
	got2, err := dl2.ReadEntryAt(lf, pos2, 0)
	if err != nil {
		t.Fatalf("ReadEntryAt(pos2=%d) error = %v", pos2, err)
	}
	if got2.Commit != 2 {
		t.Errorf("entry at pos2: commit = %d, want 2", got2.Commit)
	}
}

// An abandoned snapshot leaves a blob header holding its placeholder length of 0 —
// SnapshotWriter.Abandon releases the lock without patching it, and a crash before Close
// does the same. The blob's data, and every entry appended afterwards, sit behind it.
//
// The frame walk cannot cross that header, but what follows is live data, not a torn tail.
// Truncating there discards the entire rest of the log: against a real 185 MB verse log
// this proposed dropping 179 MB.
func TestOpenKeepsDataBeyondUnpatchedBlobHeader(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	pos1, lf, err := dl.AppendEntry(testEntry(1, "before"))
	if err != nil {
		t.Fatalf("AppendEntry(1) error = %v", err)
	}
	afterFirst := dl.logA.Position()
	if err := dl.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Abandoned snapshot: magic + unpatched length 0, then blob data nobody can measure.
	path := filepath.Join(tmpDir, "logA")
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	hdr := make([]byte, BlobHeaderSize)
	binary.BigEndian.PutUint32(hdr[0:4], BlobHeaderMagic)
	binary.BigEndian.PutUint32(hdr[4:8], 0) // never patched
	if _, err := f.WriteAt(hdr, afterFirst); err != nil {
		t.Fatalf("write blob header: %v", err)
	}
	blob := make([]byte, 512) // event bytes; not framed as records
	if _, err := f.WriteAt(blob, afterFirst+BlobHeaderSize); err != nil {
		t.Fatalf("write blob data: %v", err)
	}
	f.Close()

	sizeBefore := int64(0)
	if st, err := os.Stat(path); err == nil {
		sizeBefore = st.Size()
	}

	dl2, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() (reopen) error = %v", err)
	}
	defer dl2.Close()

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size() != sizeBefore {
		t.Fatalf("log truncated on open: %d -> %d bytes (dropped %d)",
			sizeBefore, st.Size(), sizeBefore-st.Size())
	}
	// Appends must land after everything already written, never over it.
	if got := dl2.logA.Position(); got != sizeBefore {
		t.Errorf("append position = %d, want %d (end of file)", got, sizeBefore)
	}
	if _, err := dl2.ReadEntryAt(lf, pos1, 0); err != nil {
		t.Errorf("entry before the blob header no longer reads: %v", err)
	}
	pos2, _, err := dl2.AppendEntry(testEntry(2, "after"))
	if err != nil {
		t.Fatalf("AppendEntry(2) error = %v", err)
	}
	if pos2 < sizeBefore {
		t.Errorf("append at %d overwrites existing data (file was %d bytes)", pos2, sizeBefore)
	}
	got2, err := dl2.ReadEntryAt(lf, pos2, 0)
	if err != nil {
		t.Fatalf("ReadEntryAt(pos2=%d) error = %v", pos2, err)
	}
	if got2.Commit != 2 {
		t.Errorf("entry at pos2: commit = %d, want 2", got2.Commit)
	}
}

// Opening a log never deletes. A torn tail is dealt with by where the next record goes,
// not by removing bytes: position is set to the last complete frame, so the next append
// overwrites the stump, and iteration is bounded by that same position so the stump falls
// outside it. Truncation's only job was that bound.
func TestOpenDoesNotTruncateAndIterationStaysClean(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	for i := 1; i <= 4; i++ {
		if _, _, err := dl.AppendEntry(testEntry(int64(i), "v")); err != nil {
			t.Fatalf("AppendEntry(%d) error = %v", i, err)
		}
	}
	goodEnd := dl.logA.Position()
	if err := dl.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Crash mid-append: a length prefix whose payload never landed.
	path := filepath.Join(tmpDir, "logA")
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr, 8192)
	if _, err := f.WriteAt(hdr, goodEnd); err != nil {
		t.Fatalf("write stump: %v", err)
	}
	f.Close()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	sizeWithStump := st.Size()

	dl2, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() (reopen) error = %v", err)
	}
	defer dl2.Close()

	st2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after reopen: %v", err)
	}
	if st2.Size() != sizeWithStump {
		t.Errorf("open truncated the file: %d -> %d bytes", sizeWithStump, st2.Size())
	}
	if got := dl2.logA.Position(); got != goodEnd {
		t.Errorf("append position = %d, want %d (last complete frame)", got, goodEnd)
	}

	it, err := dl2.Iterator()
	if err != nil {
		t.Fatalf("Iterator() error = %v", err)
	}
	n := 0
	for {
		e, _, _, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("iteration errored on the stump instead of stopping cleanly: %v", err)
		}
		if e != nil {
			n++
		}
	}
	if n != 4 {
		t.Errorf("iterated %d entries, want 4", n)
	}
}

// Opens a real data directory read-write, exactly as the daemon does, and requires the
// files to be the same size afterwards. Set REAL_DLOG_DIR to a COPY of one — this opens
// for write. Skipped by default; the failure this guards against was only ever visible
// against real data, where an abandoned snapshot sits mid-file.
func TestOpenRealDataDirDeletesNothing(t *testing.T) {
	dir := os.Getenv("REAL_DLOG_DIR")
	if dir == "" {
		t.Skip("REAL_DLOG_DIR not set (point it at a COPY of a data dir)")
	}
	before := map[string]int64{}
	for _, n := range []string{"logA", "logB"} {
		st, err := os.Stat(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("stat %s: %v", n, err)
		}
		before[n] = st.Size()
	}
	dl, err := NewDLog(dir, nil)
	if err != nil {
		t.Fatalf("NewDLog: %v", err)
	}
	if err := dl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, n := range []string{"logA", "logB"} {
		st, err := os.Stat(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("stat %s: %v", n, err)
		}
		if st.Size() != before[n] {
			t.Errorf("%s: %d -> %d bytes, DELETED %d", n, before[n], st.Size(), before[n]-st.Size())
		}
	}
}

// An abandoned snapshot must not leave a region the frame walk cannot cross. Abandon
// patches the blob header with what was actually written, so the orphaned blob is skipped
// like any other and the entries appended after it stay reachable. Before that, the
// unpatched placeholder stopped the walk permanently — the shape found on a real log with
// 179 MB behind it.
func TestAbandonedSnapshotLeavesAWalkableLog(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	if _, _, err := dl.AppendEntry(testEntry(1, "before")); err != nil {
		t.Fatalf("AppendEntry(1) error = %v", err)
	}

	// A snapshot on the inactive log that writes blob data and then fails.
	sw, err := dl.NewSnapshotWriter(1, "2026-07-28T10:00:00Z")
	if err != nil {
		t.Fatalf("NewSnapshotWriter() error = %v", err)
	}
	if _, err := sw.Write(make([]byte, 3000)); err != nil {
		t.Fatalf("snapshot Write error = %v", err)
	}
	sw.Abandon()

	// Make that log active and keep appending, as the daemon would.
	if err := dl.SwitchActive(); err != nil {
		t.Fatalf("SwitchActive() error = %v", err)
	}
	for i := 2; i <= 5; i++ {
		if _, _, err := dl.AppendEntry(testEntry(int64(i), "after the abandoned snapshot")); err != nil {
			t.Fatalf("AppendEntry(%d) error = %v", i, err)
		}
	}
	if err := dl.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// The frame walk must reach the end of both files: no uncrossable region.
	for _, name := range []string{"logA", "logB"} {
		p := filepath.Join(tmpDir, name)
		f, err := os.Open(p)
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		st, _ := f.Stat()
		end, torn, err := scanFrames(f, st.Size())
		f.Close()
		if err != nil {
			t.Fatalf("scanFrames(%s): %v", name, err)
		}
		if torn {
			t.Errorf("%s: walk reports a torn tail it should not have", name)
		}
		if end != st.Size() {
			t.Errorf("%s: walk stopped at %d of %d — %d bytes unreachable",
				name, end, st.Size(), st.Size()-end)
		}
	}

	// And every entry is still iterable across the orphaned blob.
	dl2, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() (reopen) error = %v", err)
	}
	defer dl2.Close()
	it, err := dl2.Iterator()
	if err != nil {
		t.Fatalf("Iterator() error = %v", err)
	}
	seen := map[int64]bool{}
	for {
		e, _, _, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("iteration failed across the abandoned snapshot: %v", err)
		}
		if e != nil {
			seen[e.Commit] = true
		}
	}
	for i := int64(1); i <= 5; i++ {
		if !seen[i] {
			t.Errorf("entry commit=%d not reachable after the abandoned snapshot", i)
		}
	}
}
