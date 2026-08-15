package stream

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// roundTrip takes a document through the event stream and back, which is what
// the log does to everything it stores.
func roundTrip(t *testing.T, src string) string {
	t.Helper()
	n, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	evs, err := NodeToEvents(n)
	if err != nil {
		t.Fatalf("NodeToEvents: %v", err)
	}
	back, err := EventsToNode(evs)
	if err != nil {
		t.Fatalf("EventsToNode: %v (events %v)", err, evs)
	}
	var b strings.Builder
	if err := encode.Encode(back, &b, encode.EncodeComments(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b.String()
}

// TestCommentsSurviveTheEventStream: a head comment wraps the value it precedes
// in a CommentType node, and rebuilding pushed that WRAPPER onto the stack
// instead of the container inside it. The next key then looked for its object
// and found a comment: "unexpected EventKey (not in object)".
//
// The log stores events, so this was not a formatting loss -- a commented
// payload was written and could never be read back, and the session that tried
// died rather than erroring.
func TestCommentsSurviveTheEventStream(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      []string
	}{
		{"a head comment before the document", "# leading\nname: svc\n", []string{"# leading", "name: svc"}},
		{"a head comment before a field's value", "items:\n# above\n- a\n", []string{"# above", "- a"}},
		{"a line comment", "name: svc # the latch\n", []string{"# the latch"}},
		{"all three at once", "# leading\nname: svc # the latch\n# above\nitems:\n- a\n",
			[]string{"# leading", "# the latch", "# above"}},
		{"a head comment on a nested object", "a:\n# about b\n  b:\n    c: 1\n", []string{"# about b", "c: 1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := roundTrip(t, tc.src)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("the round trip lost %q:\n%s", want, got)
				}
			}
		})
	}
}

// TestLineCommentIsWrittenOnce: the value's own case and its container both used
// to write it, so every line comment appeared twice in the stream. It survived
// only because the second overwrote the first with the same lines.
func TestLineCommentIsWrittenOnce(t *testing.T) {
	n, err := parse.Parse([]byte("name: svc # the latch\nother: 1 # another\n"), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	evs, err := NodeToEvents(n)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range evs {
		if e.Type == EventLineComment {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("%d line comment events for two comments:\n%v", count, evs)
	}
}
