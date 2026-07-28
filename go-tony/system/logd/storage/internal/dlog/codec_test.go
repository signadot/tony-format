package dlog

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

func sampleEntry(commit int64) *Entry {
	last := int64(commit - 1)
	return &Entry{
		Commit:    commit,
		Timestamp: "2026-07-28T09:00:00+02:00",
		Patch: ir.FromMap(map[string]*ir.Node{
			"demo": ir.FromMap(map[string]*ir.Node{
				"x":     ir.FromString("hello — em dash"),
				"n":     ir.FromInt(42),
				"flag":  ir.FromBool(true),
				"empty": ir.Null(),
				"list":  ir.FromSlice([]*ir.Node{ir.FromInt(1), ir.FromString("two")}),
			}),
		}),
		LastCommit: &last,
	}
}

func TestEncodeEntryRoundTrip(t *testing.T) {
	in := sampleEntry(9)
	b, err := encodeEntry(in)
	if err != nil {
		t.Fatalf("encodeEntry: %v", err)
	}
	if legacyTextEntry(b) {
		t.Fatalf("encodeEntry produced something that reads as legacy text: %q", b[:min(8, len(b))])
	}
	out, err := decodeEntry(b)
	if err != nil {
		t.Fatalf("decodeEntry: %v", err)
	}
	if out.Commit != in.Commit || out.Timestamp != in.Timestamp {
		t.Errorf("scalars differ: got commit=%d ts=%q", out.Commit, out.Timestamp)
	}
	if out.LastCommit == nil || *out.LastCommit != *in.LastCommit {
		t.Errorf("LastCommit lost: %v", out.LastCommit)
	}
	if out.Patch == nil {
		t.Fatal("Patch lost")
	}
	if ir.Compare(in.Patch, out.Patch) != 0 {
		t.Errorf("patch differs:\n in = %v\nout = %v", in.Patch, out.Patch)
	}
}

// The encoder must be deterministic: the log's framing records a length, and compaction
// measures an entry by what writeEntry returns, so two encodings of the same entry that
// disagreed would put every later record at the wrong offset.
func TestEncodeEntryIsDeterministic(t *testing.T) {
	e := sampleEntry(11)
	first, err := encodeEntry(e)
	if err != nil {
		t.Fatalf("encodeEntry: %v", err)
	}
	for i := 0; i < 20; i++ {
		again, err := encodeEntry(e)
		if err != nil {
			t.Fatalf("encodeEntry (%d): %v", i, err)
		}
		if string(again) != string(first) {
			t.Fatalf("encoding %d differs from the first: %d vs %d bytes", i, len(again), len(first))
		}
	}
}

// Entries written before the switch to binary are block-style tony text. They must still
// read back, so an existing log keeps working without a migration step.
func TestDecodeEntryReadsLegacyTonyText(t *testing.T) {
	in := sampleEntry(5)
	text, err := in.ToTony() // what the old writer produced
	if err != nil {
		t.Fatalf("ToTony: %v", err)
	}
	if !legacyTextEntry(text) {
		t.Fatalf("legacy text not detected as such: %q", text[:min(8, len(text))])
	}
	out, err := decodeEntry(text)
	if err != nil {
		t.Fatalf("decodeEntry(legacy text): %v", err)
	}
	if out.Commit != in.Commit || out.Timestamp != in.Timestamp {
		t.Errorf("legacy scalars differ: got commit=%d ts=%q", out.Commit, out.Timestamp)
	}
	if ir.Compare(in.Patch, out.Patch) != 0 {
		t.Errorf("legacy patch differs:\n in = %v\nout = %v", in.Patch, out.Patch)
	}
}

// End to end: a log file whose entries were written in the old text form must still open
// and read, mixed with entries appended in the new binary form.
func TestReadMixedEncodingLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logA")

	// Hand-write one legacy text entry, framed the way the old writer framed it.
	legacy := sampleEntry(1)
	text, err := legacy.ToTony()
	if err != nil {
		t.Fatalf("ToTony: %v", err)
	}
	rec := make([]byte, 4+len(text))
	binary.BigEndian.PutUint32(rec[:4], uint32(len(text)))
	copy(rec[4:], text)
	if err := os.WriteFile(path, rec, 0644); err != nil {
		t.Fatalf("write legacy log: %v", err)
	}

	dl, err := NewDLog(dir, nil)
	if err != nil {
		t.Fatalf("NewDLog: %v", err)
	}
	defer dl.Close()

	// Append a binary entry after it.
	binPos, _, err := dl.AppendEntry(sampleEntry(2))
	if err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}

	got1, err := dl.ReadEntryAt(LogFileA, 0, 0)
	if err != nil {
		t.Fatalf("read legacy entry: %v", err)
	}
	if got1.Commit != 1 {
		t.Errorf("legacy entry commit = %d, want 1", got1.Commit)
	}
	got2, err := dl.ReadEntryAt(LogFileA, binPos, 0)
	if err != nil {
		t.Fatalf("read binary entry: %v", err)
	}
	if got2.Commit != 2 {
		t.Errorf("binary entry commit = %d, want 2", got2.Commit)
	}
}

// Reports the size difference, so a regression in either direction is visible rather than
// assumed. Binary is not automatically smaller for tiny entries: every event carries a
// length-prefixed tag, which costs a byte or two even when empty.
func TestEncodingSizeComparison(t *testing.T) {
	for _, n := range []int{1, 10, 100} {
		fields := map[string]*ir.Node{}
		for i := 0; i < n; i++ {
			fields[time.Now().Format("k")+string(rune('a'+i%26))+string(rune('a'+i/26))] =
				ir.FromString("some representative value")
		}
		e := &Entry{
			Commit:    7,
			Timestamp: "2026-07-28T09:00:00+02:00",
			Patch:     ir.FromMap(map[string]*ir.Node{"demo": ir.FromMap(fields)}),
		}
		text, err := e.ToTony()
		if err != nil {
			t.Fatalf("ToTony: %v", err)
		}
		bin, err := encodeEntry(e)
		if err != nil {
			t.Fatalf("encodeEntry: %v", err)
		}
		t.Logf("%3d fields: text %5d bytes, binary %5d bytes (%+d)", n, len(text), len(bin), len(bin)-len(text))
	}
}
