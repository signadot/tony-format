package index

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

// The generated codec carries every field of a LogSegment.
//
// It went twelve days without Spine: the field was added to the struct and the codec was
// not regenerated, so ToTonyIR wrote a segment without it and FromTonyIR read one back
// with it false. Nothing failed, for two reasons worth writing down rather than
// rediscovering. The index is persisted with encoding/gob, which takes exported fields
// without being told, so the codec is not on that path -- and false is Spine's safe value
// anyway: a segment wrongly not marked spine is INCLUDED by a lookup, which is what
// lookups did before the field existed.
//
// So the drift cost nothing, and the next one might not. `make generate-verify` answers
// this too, and nothing runs it; this does, in the suite.
func TestSegmentCodecCarriesEveryField(t *testing.T) {
	scope := "s1"
	seg := LogSegment{
		KindedPath:        "a.b",
		StartCommit:       1,
		EndCommit:         2,
		StartTx:           3,
		LogFile:           "A",
		LogPosition:       4,
		LogFileGeneration: 5,
		ScopeID:           &scope,
		ScopeOverlay:      true,
		Spine:             true,
	}

	node, err := seg.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var got LogSegment
	if err := got.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}

	if got.ScopeID == nil || *got.ScopeID != scope {
		t.Errorf("ScopeID = %v, want %q", got.ScopeID, scope)
	}
	got.ScopeID, seg.ScopeID = nil, nil // compared above; the rest compare by value
	if got != seg {
		t.Errorf("round trip lost something:\n got %+v\nwant %+v", got, seg)
	}
}

// And the field a lookup reads: a segment the patch merely passed THROUGH is not a write
// to that path, so a read there skips it. That is what lets a read at one path stop
// replaying the writes to its siblings.
func TestSpineMarksAPathAPatchPassedThrough(t *testing.T) {
	for _, tc := range []struct {
		name  string
		patch string
		want  bool
	}{
		{"a plain container on the way down", "b:\n  c: 1\n", true},
		// The same document in flow style. It carries !bracket, which is how it was
		// written and not something it says about what is under it -- so it passes
		// through too, and a JSON client's patches are indexed like anyone else's.
		{"the same, written in flow", `{b: {c: 1}}`, true},
		{"a list, written in flow", `[1, 2]`, true},
		{"a scalar, which is the write itself", `1`, false},
		{"an empty container, which states emptiness", `{}`, false},
		{"an operator, which is about this node", `!delete`, false},
		{"a data tag, which this patch does assert here", `!mytag {b: 1}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := parse.Parse([]byte(tc.patch))
			if err != nil {
				t.Fatalf("parse %q: %v", tc.patch, err)
			}
			if got := passesThrough(n); got != tc.want {
				t.Errorf("passesThrough(%s) = %v, want %v", tc.patch, got, tc.want)
			}
		})
	}
}
