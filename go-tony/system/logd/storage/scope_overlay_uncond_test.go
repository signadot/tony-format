package storage

import (
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
)

// R1 says the overlay may hold no CHECKED forms, because it is re-applied to a baseline
// that is expected to have moved. libdiff.MakeDiff is the only place !replace{from,to} is
// produced, and its three branches are: from==nil -> !insert, to==nil -> !delete,
// otherwise -> !replace{from,to}. So "unconditional" is the same diff with that third
// branch answering !insert(to).
//
// This checks whether that can be done as a POST-PASS over the overlay -- which needs no
// change to Diff at all, the way key annotation needs none (scope_overlay_keyann_test) --
// rather than threading a config through a recursion that carries neither path nor config.

// unconditional rewrites checked replaces into the value they would have installed.
func unconditional(n *ir.Node) *ir.Node {
	if n == nil {
		return nil
	}
	if _, args := ir.TagGet(n.Tag, "!replace"); args != nil || ir.TagHas(n.Tag, "!replace") {
		if to := ir.Get(n, "to"); to != nil {
			return unconditional(to.Clone())
		}
	}
	for i, v := range n.Values {
		n.Values[i] = unconditional(v)
	}
	return n
}

func TestOverlayUnconditionalPostPass(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	scope := "s1"

	scalingCommit(t, s, nil, `{a: {x: 1}}`, nil)
	scalingCommit(t, s, &scope, `{a: {x: 5}}`, nil)

	overlay := overlayAt(t, s, scope)
	t.Logf("overlay as Diff produces it: %s", nodeOrNil(t, overlay))

	uncond := unconditional(overlay.Clone())
	t.Logf("after the post-pass:         %s", nodeOrNil(t, uncond))

	// Baseline moves at the very path the scope owns -- the case that errored outright.
	scalingCommit(t, s, nil, `{a: {x: 99}}`, nil)

	if _, err := applyOverlaySoft(t, s, overlay); err == nil {
		t.Errorf("expected the CHECKED overlay to fail against a moved baseline")
	} else {
		t.Logf("checked overlay against moved baseline: %v", err)
	}

	viaOverlay, err := applyOverlaySoft(t, s, uncond)
	if err != nil {
		t.Fatalf("unconditional overlay failed to apply: %v", err)
	}
	viaReplay := showDocQuiet(t, s, &scope)
	t.Logf("via unconditional overlay: %s", viaOverlay)
	t.Logf("via replay:                %s", viaReplay)
	if viaOverlay != viaReplay {
		t.Errorf("unconditional overlay disagrees with replay\n got  %s\n want %s", viaOverlay, viaReplay)
	}
}

// TestOverlayUnconditionalTypeChange covers MakeDiff's other reason to emit a checked
// replace: the value changed TYPE, here a container replaced by a scalar.
func TestOverlayUnconditionalTypeChange(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	scope := "s1"

	scalingCommit(t, s, nil, `{a: {x: 1, y: 2}}`, nil)
	scalingCommit(t, s, &scope, `{a: "scalar"}`, nil)

	overlay := unconditional(overlayAt(t, s, scope).Clone())
	t.Logf("unconditional overlay: %s", nodeOrNil(t, overlay))

	scalingCommit(t, s, nil, `{a: {x: 42, z: 7}}`, nil)

	viaOverlay, err := applyOverlaySoft(t, s, overlay)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	viaReplay := showDocQuiet(t, s, &scope)
	t.Logf("via overlay: %s", viaOverlay)
	t.Logf("via replay:  %s", viaReplay)
	if viaOverlay != viaReplay {
		t.Errorf("disagree\n got  %s\n want %s", viaOverlay, viaReplay)
	}
}

var _ = tony.Diff
