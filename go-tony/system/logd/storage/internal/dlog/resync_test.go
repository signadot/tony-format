package dlog

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// iterate walks both logs and answers the commits it found, in order.
func iterate(t *testing.T, dl *DLog) ([]int64, []Gap) {
	t.Helper()
	it, err := dl.Iterator()
	if err != nil {
		t.Fatalf("Iterator: %v", err)
	}
	var commits []int64
	for {
		e, _, _, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		commits = append(commits, e.Commit)
	}
	return commits, it.Gaps()
}

// writeUnpatchedBlob puts an interrupted snapshot's signature at the end of a log file: a
// blob header whose length was never patched, then bytes that are not framed records.
func writeUnpatchedBlob(t *testing.T, path string, junk int) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	hdr := make([]byte, BlobHeaderSize)
	binary.BigEndian.PutUint32(hdr[0:4], BlobHeaderMagic)
	binary.BigEndian.PutUint32(hdr[4:8], 0)
	if _, err := f.WriteAt(hdr, st.Size()); err != nil {
		t.Fatal(err)
	}
	blob := make([]byte, junk)
	for i := range blob {
		blob[i] = byte(0x41 + i%23) // event-shaped bytes, not framed records
	}
	if _, err := f.WriteAt(blob, st.Size()+BlobHeaderSize); err != nil {
		t.Fatal(err)
	}
}

// An OOM in the middle of a snapshot leaves the log that had just gone inactive ending
// in an unpatched blob, and the active log with a torn tail -- both files damaged at
// once. Every entry in both must still be walked: the walk steps over what it cannot
// read and ends a file where nothing readable follows, and a torn tail falls outside
// the append frontier as before.
func TestWalkSurvivesBothLogsDamaged(t *testing.T) {
	dir := t.TempDir()
	dl, err := NewDLog(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if _, _, err := dl.AppendEntry(testEntry(int64(i), "a")); err != nil {
			t.Fatal(err)
		}
	}
	if err := dl.SwitchActive(); err != nil { // A goes inactive, B takes the appends
		t.Fatal(err)
	}
	for i := 4; i <= 6; i++ {
		if _, _, err := dl.AppendEntry(testEntry(int64(i), "b")); err != nil {
			t.Fatal(err)
		}
	}
	if err := dl.Close(); err != nil {
		t.Fatal(err)
	}
	// The snapshot of A that never finished, and the record on B that never landed.
	writeUnpatchedBlob(t, filepath.Join(dir, "logA"), 3000)
	pathB := filepath.Join(dir, "logB")
	st, _ := os.Stat(pathB)
	if err := os.Truncate(pathB, st.Size()-5); err != nil {
		t.Fatal(err)
	}

	dl2, err := NewDLog(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer dl2.Close()
	commits, gaps := iterate(t, dl2)
	want := []int64{1, 2, 3, 4, 5}
	if len(commits) != len(want) {
		t.Fatalf("walked commits %v, want %v (the torn record on B is the one loss)", commits, want)
	}
	for i := range want {
		if commits[i] != want[i] {
			t.Fatalf("walked commits %v, want %v", commits, want)
		}
	}
	if len(gaps) != 1 || gaps[0].LogFile != LogFileA {
		t.Errorf("gaps %v, want one on logA", gaps)
	}
	// And the store keeps going: appends after the reopen are walked too.
	if _, _, err := dl2.AppendEntry(testEntry(7, "after")); err != nil {
		t.Fatal(err)
	}
	if err := dl2.SwitchActive(); err != nil { // back onto A, behind its dead blob
		t.Fatal(err)
	}
	if _, _, err := dl2.AppendEntry(testEntry(8, "behind the blob")); err != nil {
		t.Fatal(err)
	}
	commits, _ = iterate(t, dl2)
	if len(commits) != 7 || commits[6] != 8 {
		t.Errorf("after appending behind the dead blob, walked %v", commits)
	}
}

// A log written by a process that continued after an abandoned snapshot holds entries
// BEHIND the unpatched blob. Those are real and are walked.
func TestWalkResumesBehindAnUnpatchedBlob(t *testing.T) {
	dir := t.TempDir()
	dl, err := NewDLog(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := dl.AppendEntry(testEntry(1, "before")); err != nil {
		t.Fatal(err)
	}
	if err := dl.Close(); err != nil {
		t.Fatal(err)
	}
	writeUnpatchedBlob(t, filepath.Join(dir, "logA"), 700)

	dl2, err := NewDLog(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 4; i++ {
		if _, _, err := dl2.AppendEntry(testEntry(int64(i), "behind")); err != nil {
			t.Fatal(err)
		}
	}
	if err := dl2.Close(); err != nil {
		t.Fatal(err)
	}

	dl3, err := NewDLog(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dl3.Close()
	commits, gaps := iterate(t, dl3)
	if len(commits) != 4 {
		t.Fatalf("walked %v, want 1 through 4", commits)
	}
	if len(gaps) != 1 || gaps[0].From == gaps[0].To {
		t.Errorf("gaps %v, want the blob's region", gaps)
	}
}

// A record that will not decode, with good records behind it, is stepped over and the
// rest is walked.
func TestWalkStepsOverAnUndecodableRecord(t *testing.T) {
	dir := t.TempDir()
	dl, err := NewDLog(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := dl.AppendEntry(testEntry(1, "good")); err != nil {
		t.Fatal(err)
	}
	pos, _, err := dl.AppendEntry(testEntry(2, "to be smashed"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := dl.AppendEntry(testEntry(3, "good again")); err != nil {
		t.Fatal(err)
	}
	if err := dl.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "logA"), os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("\xff\xfe\xfd\xfc\xfb\xfa"), pos+4); err != nil { // the payload, not the length
		t.Fatal(err)
	}
	f.Close()

	dl2, err := NewDLog(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dl2.Close()
	commits, gaps := iterate(t, dl2)
	if len(commits) != 2 || commits[0] != 1 || commits[1] != 3 {
		t.Fatalf("walked %v, want [1 3]", commits)
	}
	if len(gaps) != 1 {
		t.Errorf("gaps %v, want one", gaps)
	}
}
