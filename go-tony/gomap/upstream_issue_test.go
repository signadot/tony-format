package gomap_test

// Regression tests for the tony-codegen/gomap defects tracked in issue
// f69agjyeh12ksa25cnn0 (hand-written ToTonyIR codecs unreachable/unusable in
// several common shapes). Each test names the issue item it guards.

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
)

// item1Leaf has a VALUE-receiver ToTonyIR — the shape that reflection dispatch
// used to skip for non-pointer fields.
type item1Leaf struct{ V string }

func (l item1Leaf) ToTonyIR(opts ...gomap.MapOption) (*ir.Node, error) {
	return ir.FromString("LEAF"), nil
}

type item1Host struct {
	ValLeaf item1Leaf  `tony:"field=valLeaf"`
	PtrLeaf *item1Leaf `tony:"field=ptrLeaf"`
}

// Item 1: gomap reflection must dispatch to ToTonyIR for value fields, not only
// pointer fields.
func TestUpstreamItem1_ValueToTonyIRDispatched(t *testing.T) {
	n, err := gomap.ToTonyIR(&item1Host{PtrLeaf: &item1Leaf{}})
	if err != nil {
		t.Fatal(err)
	}
	valLeaf, _ := n.GetPath("$.valLeaf")
	ptrLeaf, _ := n.GetPath("$.ptrLeaf")
	if valLeaf == nil || valLeaf.String != "LEAF" {
		t.Errorf("value field not dispatched to ToTonyIR: got %s", encode.MustString(n))
	}
	if ptrLeaf == nil || ptrLeaf.String != "LEAF" {
		t.Errorf("pointer field not dispatched to ToTonyIR: got %s", encode.MustString(n))
	}
}

// item2Match is a NAMED MAP type with a hand-written codec — the natural Go
// spelling for an open-ended fragment that still needs custom encoding.
type item2Match map[string]any

func (m item2Match) ToTonyIR(opts ...gomap.MapOption) (*ir.Node, error) {
	return ir.FromString("MATCH"), nil
}

type item2Host struct {
	M item2Match `tony:"field=m"`
}

// Item 2 (reflection path): a named map type's own codec must be dispatched to,
// not inlined as a raw map. Fixed together with item 1.
func TestUpstreamItem2_NamedMapCodecDispatched_Reflection(t *testing.T) {
	n, err := gomap.ToTonyIR(&item2Host{M: item2Match{"a": 1}})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := n.GetPath("$.m")
	if m == nil || m.String != "MATCH" {
		t.Errorf("named map codec not dispatched: got %s", encode.MustString(n))
	}
}

// item1FromLeaf has both codecs; its FromTonyIR marks the value so a test can tell
// whether the reflection decode dispatched to it or walked the struct.
type item1FromLeaf struct{ V string }

func (l item1FromLeaf) ToTonyIR(opts ...gomap.MapOption) (*ir.Node, error) {
	return ir.FromString("L:" + l.V), nil
}

func (l *item1FromLeaf) FromTonyIR(n *ir.Node, opts ...gomap.UnmapOption) error {
	l.V = "decoded:" + n.String
	return nil
}

type item1FromHost struct {
	Val item1FromLeaf  `tony:"field=val"`
	Ptr *item1FromLeaf `tony:"field=ptr"`
}

// TestUpstreamItem1_FromValueDispatched guards the FROM-direction half of item 1:
// gomap reflection must dispatch to FromTonyIR for a value field, not only a
// pointer field — item 1 fixed only the ToTonyIR (encode) side, leaving decode
// with the exact asymmetry it removed from encode.
func TestUpstreamItem1_FromValueDispatched(t *testing.T) {
	node, err := gomap.ToTonyIR(&item1FromHost{Val: item1FromLeaf{V: "a"}, Ptr: &item1FromLeaf{V: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	var got item1FromHost
	if err := gomap.FromTonyIR(node, &got); err != nil {
		t.Fatal(err)
	}
	if got.Val.V != "decoded:L:a" {
		t.Errorf("value field not decoded via FromTonyIR: %q", got.Val.V)
	}
	if got.Ptr == nil || got.Ptr.V != "decoded:L:b" {
		t.Errorf("pointer field not decoded via FromTonyIR: %v", got.Ptr)
	}
}
