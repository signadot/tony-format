package mergeop_test

import (
	"bytes"
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func render(t *testing.T, n *ir.Node) string {
	t.Helper()
	var b bytes.Buffer
	if err := encode.Encode(n, &b, encode.EncodeWire(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b.String()
}

// A tag diff decorates the diff of the VALUE, and a bare null under it says the value did
// not change -- only its tag did. That test used to require the null to carry NO tag at
// all, and a tag chain has room for a label after the operation:
//
//	!addtag(bracket).logd-patch-root null
//
// SplitChild takes the first registered op and hands everything after it to the child, so
// the trailing label became the child's tag, the child stopped being a bare null, and the
// op patched the document with a tagged null instead of keeping it. Applied to a whole
// document that is total loss, and logd reaches it: a no-op !rename at the document root
// lowers to exactly this shape and the store read back null (1hf5pzj6h12ksd40jdn0).
//
// What decides is whether an OPERATION trails the tag op, not whether anything does --
// see TestATagDiffStillRunsAnOperationComposedAfterIt, which is the other half of this and
// the reason the test cannot simply be "the child is null". A trailing label naming no
// operation is not the value speaking: a diff composes the tag op LAST, after the value's
// own tags (libdiff/object.go), and a value that BECOMES null is emitted as !replace
// rather than as a tag op over a null.
func TestATagDiffKeepsTheValueWhateverTrailsTheOp(t *testing.T) {
	const doc = `{k1: 5, k2: 16}`
	for _, patch := range []string{
		`!addtag(bracket) null`,
		`!addtag(bracket).logd-patch-root null`,
		`!rmtag(bracket) null`,
		`!rmtag(bracket).logd-patch-root null`,
		`!retag(bracket,flow) null`,
		`!retag(bracket,flow).logd-patch-root null`,
	} {
		t.Run(patch, func(t *testing.T) {
			d, err := parse.Parse([]byte(doc))
			if err != nil {
				t.Fatalf("parse doc: %v", err)
			}
			p, err := parse.Parse([]byte(patch))
			if err != nil {
				t.Fatalf("parse patch: %v", err)
			}
			got, err := tony.Patch(d, p)
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			if got == nil {
				t.Fatalf("the document was destroyed; it holds %s", doc)
			}
			// The fields are what must survive; the tag is the op's business.
			for _, want := range []string{"k1", "5", "k2", "16"} {
				if !strings.Contains(render(t, got), want) {
					t.Fatalf("the value did not survive the tag diff: %s", render(t, got))
				}
			}
		})
	}
}

// The other half. A tag op composed over a real operation is an ordinary composition and
// the operation has to run, even when its operand is null -- `!retag(a,b).insert null`
// inserts a null and means it. Reading "the child is null" as "the value did not change"
// without asking what trails the op discards those silently, which is the same class of
// loss as the bug above and in the opposite direction.
func TestATagDiffStillRunsAnOperationComposedAfterIt(t *testing.T) {
	const doc = `{k1: 5, k2: 16}`
	for _, tc := range []struct{ patch, want string }{
		{`!retag(bracket,flow).insert 5`, `!flow 5`},
		{`!retag(bracket,flow).insert null`, `!flow null`},
		{`!addtag(x).insert null`, `!x null`},
	} {
		t.Run(tc.patch, func(t *testing.T) {
			d, err := parse.Parse([]byte(doc))
			if err != nil {
				t.Fatalf("parse doc: %v", err)
			}
			p, err := parse.Parse([]byte(tc.patch))
			if err != nil {
				t.Fatalf("parse patch: %v", err)
			}
			got, err := tony.Patch(d, p)
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			if render(t, got) != tc.want {
				t.Errorf("got %s, want %s -- the composed operation did not run",
					render(t, got), tc.want)
			}
		})
	}
	// And a delete composed after one still deletes.
	d, err := parse.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := parse.Parse([]byte(`!addtag(x).delete null`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := tony.Patch(d, p)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if got != nil {
		t.Errorf("a delete composed after a tag op left %s", render(t, got))
	}
}
