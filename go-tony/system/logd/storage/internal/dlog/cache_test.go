package dlog

import (
	"encoding/binary"
	"testing"
)

// SnapshotStart answers the snapshot's blob header, the first byte of the snapshot, and
// not its data or its entry; DropCache over everything before it, and over the file a
// compaction retired, leaves every record reading as it did.
func TestSnapshotStartAndDropCache(t *testing.T) {
	dl, err := NewDLog(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewDLog: %v", err)
	}
	defer dl.Close()

	patchPos, _, err := dl.AppendEntry(testEntry(1, "before"))
	if err != nil {
		t.Fatalf("AppendEntry(1): %v", err)
	}
	if err := dl.SwitchActive(); err != nil {
		t.Fatalf("SwitchActive: %v", err)
	}
	sw, err := dl.NewSnapshotWriter(1, "2026-11-02T10:00:00Z")
	if err != nil {
		t.Fatalf("NewSnapshotWriter: %v", err)
	}
	if _, err := sw.Write(make([]byte, 3000)); err != nil {
		t.Fatalf("snapshot Write: %v", err)
	}
	if err := sw.Close(); err != nil {
		t.Fatalf("snapshot Close: %v", err)
	}
	snapLog, snapEntryPos := sw.LogFileID(), sw.EntryPosition()

	gen := dl.GetGeneration(snapLog)
	start, err := dl.SnapshotStart(snapLog, snapEntryPos, gen)
	if err != nil {
		t.Fatalf("SnapshotStart: %v", err)
	}
	lf, _ := dl.fileFor(snapLog)
	word := make([]byte, 4)
	if _, err := lf.file.ReadAt(word, start); err != nil {
		t.Fatalf("read at %d: %v", start, err)
	}
	if !isBlobMagic(binary.BigEndian.Uint32(word)) {
		t.Fatalf("SnapshotStart = %d, which is not a blob header", start)
	}
	if start <= patchPos {
		t.Fatalf("SnapshotStart = %d, at or before the patch at %d ahead of it", start, patchPos)
	}
	if _, err := dl.SnapshotStart(snapLog, patchPos, gen); err == nil {
		t.Fatalf("SnapshotStart of a patch answered no error")
	}

	// Compacting the snapshot's log retires the file it was, and DropCache drops that too.
	results, err := dl.CompactInactive([]int64{patchPos, snapEntryPos}, nil)
	if err != nil {
		t.Fatalf("CompactInactive: %v", err)
	}
	newGen := dl.GetGeneration(snapLog)
	dl.DropCache(snapLog, gen, start) // a generation gone: the retired file alone
	dl.DropCache(snapLog, newGen, start)
	dl.DropCache(dl.GetActiveLog(), dl.GetGeneration(dl.GetActiveLog()), dl.SnapMark())

	if _, err := dl.ReadEntryAt(snapLog, patchPos, gen); err != nil {
		t.Errorf("the patch does not read from the retired file after DropCache: %v", err)
	}
	for _, r := range results {
		if _, err := dl.ReadEntryAt(snapLog, r.NewPosition, newGen); err != nil {
			t.Errorf("the record at %d does not read after DropCache: %v", r.NewPosition, err)
		}
	}
}
