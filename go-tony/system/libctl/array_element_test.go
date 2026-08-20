package libctl

import (
	"context"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// An array element is addressable: `a.votes[0]` names the first vote, and a client may write
// it and read it back. Through docd it could do neither.
//
// docd decomposes a patch by mount, and it asked pathFields for the client's path -- which
// refuses any non-field segment -- BEFORE routing, so every write to an array element was
// refused with "non-field segment [0]", mounted or not, whether or not the array existed.
// A mount path is field-only, so nothing can be mounted at or below an index: such a path
// has exactly one owner and needs no decomposition at all.
//
// And logd's read walked object fields only, so a read at the same path was refused as a bad
// segment while `o get 'a.votes[1]'` had always worked (yy0cfe9mh12kr6pwgsn0).
func TestArrayElementWriteAndRead(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "verse.items", &watchingLogdController{newLogdController(t, logd.TCPAddr(), "ctrl")})
	client := docdClient(t, docd, "client")
	ctx := context.Background()

	seed, err := parse.Parse([]byte(`{votes: [10, 20, 30]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"verse.items.a", "other.thing"} {
		if _, err := client.Patch(ctx, p, seed); err != nil {
			t.Fatalf("seed %s: %s", p, err)
		}
	}

	for _, tc := range []struct {
		name, path, parent string
		want               int64
	}{
		{"under a mount", "verse.items.a.votes[0]", "verse.items.a.votes", 99},
		{"on base", "other.thing.votes[1]", "other.thing.votes", 99},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := client.Patch(ctx, tc.path, ir.FromInt(tc.want)); err != nil {
				t.Fatalf("write %s: %s", tc.path, err)
			}
			// read the element itself
			got, err := client.Match(ctx, tc.path)
			if err != nil {
				t.Fatalf("read %s: %s", tc.path, err)
			}
			if got == nil || got.Int64 == nil || *got.Int64 != tc.want {
				t.Errorf("%s reads back as %s, want %d", tc.path, showNode(got), tc.want)
			}
			// and the array around it, so the write landed in the right slot
			arr, err := client.Match(ctx, tc.parent)
			if err != nil {
				t.Fatalf("read %s: %s", tc.parent, err)
			}
			t.Logf("%s = %s", tc.parent, showNode(arr))
		})
	}

	// An element that is not there is not there -- an answer, not a bad path.
	if _, err := client.Match(ctx, "other.thing.votes[9]"); err == nil {
		t.Error("reading past the end of an array answered without an error")
	} else if code := api.ErrorCode(err); code != api.ErrCodeNotFound {
		t.Errorf("reading past the end answered %q, want %q: %s", code, api.ErrCodeNotFound, err)
	}
}
