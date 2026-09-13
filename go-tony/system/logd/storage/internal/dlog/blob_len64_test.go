package dlog

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A snapshot of 4 GiB or more has its length in its header. The header held a uint32,
// which wrapped silently, and the walk on open read the log from the wrong offset
// (p478tacqh12krg32msn0 item 15). The blob here is sparse: one byte written past the
// 4 GiB mark, so the file claims the size without the disk holding it.
func TestASnapshotOverFourGiBIsWalkedPast(t *testing.T) {
	// A package test does not write 4 GiB, sparse or not: a file system without
	// sparse files would, and the old code this guards against walks it a byte at a
	// time. GOTEST_LONG=1 runs it.
	if os.Getenv("GOTEST_LONG") == "" {
		t.Skip("writes a sparse file past 4 GiB; set GOTEST_LONG=1 to run it")
	}
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog: %v", err)
	}
	if _, _, err := dl.AppendEntry(testEntry(1, "before")); err != nil {
		t.Fatalf("AppendEntry(1): %v", err)
	}
	if err := dl.SwitchActive(); err != nil {
		t.Fatalf("SwitchActive: %v", err)
	}
	sw, err := dl.NewSnapshotWriter(1, time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("NewSnapshotWriter: %v", err)
	}
	const big = int64(1)<<32 + 16
	if _, err := sw.Seek(big-1, io.SeekCurrent); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if _, err := sw.Write([]byte{1}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := sw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	snapEntryPos, snapLog := sw.EntryPosition(), sw.LogFileID()
	if err := dl.SwitchActive(); err != nil {
		t.Fatalf("SwitchActive back: %v", err)
	}
	if _, _, err := dl.AppendEntry(testEntry(2, "after")); err != nil {
		t.Fatalf("AppendEntry(2): %v", err)
	}
	if err := dl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dl2, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer dl2.Close()
	snap, err := dl2.ReadEntryAt(snapLog, snapEntryPos, 0)
	if err != nil {
		t.Fatalf("the snapshot's entry does not read after reopen: %v", err)
	}
	if snap.SnapPos == nil || snapEntryPos-*snap.SnapPos != big {
		t.Fatalf("snapshot entry SnapPos %v, want the blob %d bytes before its entry at %d", snap.SnapPos, big, snapEntryPos)
	}
	it, err := dl2.Iterator()
	if err != nil {
		t.Fatal(err)
	}
	var commits []int64
	for {
		e, _, _, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		commits = append(commits, e.Commit)
	}
	if len(commits) != 3 {
		t.Errorf("walk read commits %v, want the entry before, the snapshot's and the entry after", commits)
	}
	if gaps := it.Gaps(); len(gaps) != 0 {
		t.Errorf("walk skipped regions it could not read: %v", gaps)
	}
}

// A log written before the long header holds snapshots under the short one, and it
// opens, walks and compacts as it did.
func TestAShortBlobHeaderStillReadsAndCompacts(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog: %v", err)
	}
	if _, _, err := dl.AppendEntry(testEntry(1, "before")); err != nil {
		t.Fatal(err)
	}
	// The short form, as an older writer left it: magic, uint32 length, blob, then the
	// snapshot's entry pointing at the blob.
	blob := []byte("snapshot bytes")
	headerPos := dl.logA.Position()
	hdr := make([]byte, BlobHeaderSize)
	binary.BigEndian.PutUint32(hdr[0:4], BlobHeaderMagic)
	binary.BigEndian.PutUint32(hdr[4:8], uint32(len(blob)))
	if _, err := dl.logA.file.WriteAt(append(hdr, blob...), headerPos); err != nil {
		t.Fatal(err)
	}
	dl.logA.position = headerPos + BlobHeaderSize + int64(len(blob))
	snapPos := headerPos + BlobHeaderSize
	snapEntry := testEntry(2, "snap")
	snapEntry.Patch = nil
	snapEntry.SnapPos = &snapPos
	snapAt, _, err := dl.AppendEntry(snapEntry)
	if err != nil {
		t.Fatal(err)
	}
	if err := dl.Close(); err != nil {
		t.Fatal(err)
	}
	dl2, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer dl2.Close()
	if e, err := dl2.ReadEntryAt(LogFileA, snapAt, 0); err != nil || e.SnapPos == nil || *e.SnapPos != snapPos {
		t.Fatalf("the short-header snapshot's entry: %v, %v", e, err)
	}
	if err := dl2.SwitchActive(); err != nil {
		t.Fatal(err)
	}
	results, err := dl2.CompactInactive([]int64{snapAt}, &CompactConfig{GracePeriod: time.Millisecond})
	if err != nil {
		t.Fatalf("compacting a short-header snapshot: %v", err)
	}
	got, err := dl2.ReadEntryAt(LogFileA, results[0].NewPosition, dl2.GetGeneration(LogFileA))
	if err != nil || got.SnapPos == nil {
		t.Fatalf("after compaction: %v, %v", got, err)
	}
	f, err := os.Open(filepath.Join(tmpDir, "logA"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data := make([]byte, len(blob))
	if _, err := f.ReadAt(data, *got.SnapPos); err != nil || string(data) != string(blob) {
		t.Errorf("blob after compaction reads %q (%v), want %q", data, err, blob)
	}
}
