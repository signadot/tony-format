package libctl

import (
	"context"
	"errors"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// nestObj builds {k0: {k1: {... : leaf}}} with RAW field keys, mirroring how
// gomap.ToTonyIR builds a precondition pattern from a Go map (no quoting).
func nestObj(leaf *ir.Node, keys ...string) *ir.Node {
	cur := leaf
	for i := len(keys) - 1; i >= 0; i-- {
		cur = ir.FromMap(map[string]*ir.Node{keys[i]: cur})
	}
	return cur
}

// TestPreconditionQuotedKind reproduces the residual §8 stall: a root-anchored CAS
// precondition over a digit-first subtree (vote.9digit...) must hold after that
// subtree is written, exactly as a letter-first one does — direct to logd and
// through docd.
func TestPreconditionQuotedKind(t *testing.T) {
	run := func(t *testing.T, sess *LogdSession, kind, seedPath string) {
		ctx := context.Background()
		leaf := ir.FromMap(map[string]*ir.Node{"choice": ir.FromString("approve")})
		// Seed the stored doc via seedPath. A deep per-kind path
		// (vote."9digitkind".alice) is how docd's split writes a mounted subtree:
		// fieldsToKPath quotes the digit-first segment, so logd must still store the
		// key canonically (unquoted) for the unquoted precondition pattern to match.
		if _, err := sess.Patch(ctx, seedPath, leaf); err != nil {
			t.Fatalf("seed patch: %v", err)
		}
		// root-anchored precondition matching the same subtree
		pattern := nestObj(leaf, "vote", kind, "alice")
		match := &api.PathData{Path: "", Data: pattern}
		// patch: bump an unrelated field, only if precondition holds
		_, err := sess.PatchIf(ctx, "committed", ir.FromString("yes"), match)
		if err != nil {
			if errors.Is(err, ErrMatchFailed) {
				t.Fatalf("PRECONDITION FAILED for kind %q (should have held)", kind)
			}
			t.Fatalf("PatchIf kind %q: %v", kind, err)
		}
	}

	// seedPath is the canonical kpath vote.<kind>.alice — kpath.Field quotes a
	// digit-first segment, exactly as docd's fieldsToKPath does when it splits a
	// mounted subtree write.
	seedPath := func(kind string) string {
		// kpath.Field quotes a digit-first segment; join segments with "." — a
		// digit-first kind yields vote."9digitkind".alice, exactly fieldsToKPath.
		return "vote." + kpath.Field(kind).String() + ".alice"
	}

	t.Run("direct-logd/letterFirst", func(t *testing.T) {
		logd := startLogd(t)
		s := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "c"})
		t.Cleanup(func() { s.Close() })
		run(t, s, "letterkind", seedPath("letterkind"))
	})
	t.Run("direct-logd/digitFirst", func(t *testing.T) {
		logd := startLogd(t)
		s := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "c"})
		t.Cleanup(func() { s.Close() })
		run(t, s, "9digitkind", seedPath("9digitkind"))
	})
	t.Run("over-docd/letterFirst", func(t *testing.T) {
		logd := startLogd(t)
		docd := startDocdRouting(t, logd.TCPAddr())
		run(t, docdClient(t, docd, "c"), "letterkind", seedPath("letterkind"))
	})
	t.Run("over-docd/digitFirst", func(t *testing.T) {
		logd := startLogd(t)
		docd := startDocdRouting(t, logd.TCPAddr())
		run(t, docdClient(t, docd, "c"), "9digitkind", seedPath("9digitkind"))
	})
}
