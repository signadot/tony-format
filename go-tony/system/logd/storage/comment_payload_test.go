package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// TestCommentedPayloadIsReadable: a payload carrying comments used to be
// ACCEPTED and then be unreadable -- the entry it wrote could not be
// deserialized, so the session that read it died with "bad literal" rather than
// erroring. The log stores events, and the event stream could not rebuild a
// document whose container carried a head comment.
func TestCommentedPayloadIsReadable(t *testing.T) {
	const src = "# leading comment\nname: svc      # the latch\n# above the field\nitems:\n- a\n- b\n"

	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	d, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tx, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: d}})
	if err != nil {
		t.Fatalf("NewPatcher: %v", err)
	}
	r := p.Commit()
	if !r.Committed {
		t.Fatalf("commit: %v", r.Error)
	}

	got, err := s.ReadStateAt("", r.Commit, nil)
	if err != nil {
		t.Fatalf("read of a commented payload: %v", err)
	}
	var b strings.Builder
	if err := encode.Encode(got, &b, encode.EncodeComments(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := b.String()
	for _, want := range []string{"name: svc", "- a", "- b"} {
		if !strings.Contains(out, want) {
			t.Errorf("the data did not survive: %q missing from\n%s", want, out)
		}
	}
	// The comments survive too. A store keeps what it is given, so there is no
	// policy here to get wrong and nothing to configure: writing through
	// api.NextState keeps them, and a caller who wants data alone strips it from
	// the answer. This used to assert the opposite, when every patch stripped
	// them and a flag was going to decide it (3cdjz00jh12krns4g1n0).
	for _, want := range []string{"# the latch", "# leading comment", "# above the field"} {
		if !strings.Contains(out, want) {
			t.Errorf("a comment did not survive the store: %q missing from\n%s", want, out)
		}
	}
}
