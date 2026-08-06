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

// Item20Pattern stands in for a codegen'd type: it has codecs of its own, and its
// wire form is an object, so a struct embedding it can merge that object.
type Item20Pattern struct {
	System string `tony:"field=system"`
	Op     string `tony:"field=op"`
}

func (p Item20Pattern) ToTonyIR(opts ...gomap.MapOption) (*ir.Node, error) {
	return ir.FromMap(map[string]*ir.Node{
		"system": ir.FromString("S:" + p.System),
		"op":     ir.FromString(p.Op),
	}), nil
}

func (p *Item20Pattern) FromTonyIR(n *ir.Node, opts ...gomap.UnmapOption) error {
	if s, _ := n.GetPath("$.system"); s != nil {
		p.System = "decoded:" + s.String
	}
	if o, _ := n.GetPath("$.op"); o != nil {
		p.Op = o.String
	}
	return nil
}

// Item20Cover EMBEDS a codec-bearing type and adds a field of its own. Go promotes
// Item20Pattern's ToTonyIR onto it, which is what used to swallow Slot.
type Item20Cover struct {
	Item20Pattern
	Slot []string `tony:"field=slot,omitzero"`
}

// Item 20: a struct embedding a type with its own codec must keep its OWN fields.
// The promoted method belongs to the embedded field, not to the outer struct, so
// dispatching on it encoded the embedded value as the whole struct and dropped the
// siblings — silently, and symmetrically on decode, so a round trip agreed with
// itself and was wrong.
func TestUpstreamItem20_EmbeddedCodecKeepsSiblings(t *testing.T) {
	n, err := gomap.ToTonyIR(&Item20Cover{
		Item20Pattern: Item20Pattern{System: "git", Op: "add"},
		Slot:          []string{"o", "E"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The embedded codec still runs: "S:" proves the merged keys came from it and
	// were not walked structurally.
	system, _ := n.GetPath("$.system")
	if system == nil || system.String != "S:git" {
		t.Errorf("embedded codec not used for its own keys: got %s", encode.MustString(n))
	}
	slot, _ := n.GetPath("$.slot")
	if slot == nil || len(slot.Values) != 2 {
		t.Errorf("outer struct's own field lost to the embedded codec: got %s", encode.MustString(n))
	}
}

// TestUpstreamItem20_EmbeddedCodecRoundTrips guards the decode half: the embedded
// type's FromTonyIR is handed the whole object, and the outer struct's own fields
// are decoded over the top of it.
func TestUpstreamItem20_EmbeddedCodecRoundTrips(t *testing.T) {
	orig := &Item20Cover{
		Item20Pattern: Item20Pattern{System: "git", Op: "add"},
		Slot:          []string{"o", "E"},
	}
	node, err := gomap.ToTonyIR(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got Item20Cover
	if err := gomap.FromTonyIR(node, &got); err != nil {
		t.Fatal(err)
	}
	if got.System != "decoded:S:git" {
		t.Errorf("embedded codec not used on decode: System = %q", got.System)
	}
	if got.Op != "add" {
		t.Errorf("embedded field lost on decode: Op = %q", got.Op)
	}
	if len(got.Slot) != 2 || got.Slot[0] != "o" || got.Slot[1] != "E" {
		t.Errorf("outer struct's own field lost on decode: Slot = %v", got.Slot)
	}
}

// Item20Shadow embeds a codec-bearing type AND declares codecs of its own, which
// shadow the promoted ones. Its own must still win — the fix must not mistake a
// declared method for a promoted one.
type Item20Shadow struct {
	Item20Pattern
	Slot []string `tony:"field=slot"`
}

func (s Item20Shadow) ToTonyIR(opts ...gomap.MapOption) (*ir.Node, error) {
	return ir.FromString("SHADOW"), nil
}

func (s *Item20Shadow) FromTonyIR(n *ir.Node, opts ...gomap.UnmapOption) error {
	s.Slot = []string{"from:" + n.String}
	return nil
}

// TestUpstreamItem20_DeclaredCodecStillWins is the other side of the fix: a type
// that declares its own codec must still be dispatched to, even when it also
// embeds a type that has one.
func TestUpstreamItem20_DeclaredCodecStillWins(t *testing.T) {
	n, err := gomap.ToTonyIR(&Item20Shadow{Item20Pattern: Item20Pattern{System: "git"}})
	if err != nil {
		t.Fatal(err)
	}
	if n.String != "SHADOW" {
		t.Errorf("declared codec treated as promoted and skipped: got %s", encode.MustString(n))
	}
	var got Item20Shadow
	if err := gomap.FromTonyIR(ir.FromString("x"), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Slot) != 1 || got.Slot[0] != "from:x" {
		t.Errorf("declared FromTonyIR treated as promoted and skipped: Slot = %v", got.Slot)
	}
}

// Item20Text is embedded for its MarshalText, the same promotion trap one interface
// over: the outer struct would encode as a bare string.
type Item20Text struct{ V string }

func (t Item20Text) MarshalText() ([]byte, error)  { return []byte("T:" + t.V), nil }
func (t *Item20Text) UnmarshalText(b []byte) error { t.V = string(b); return nil }

type Item20TextCover struct {
	Item20Text
	Slot string `tony:"field=slot"`
}

// TestUpstreamItem20_EmbeddedTextMarshalerKeepsSiblings covers the same defect for
// encoding.TextMarshaler: a promoted MarshalText rendered the whole outer struct as
// a string. An embedded type whose wire form is a scalar cannot be merged into its
// parent, so this is now an error rather than silent loss.
func TestUpstreamItem20_EmbeddedTextMarshalerKeepsSiblings(t *testing.T) {
	_, err := gomap.ToTonyIR(&Item20TextCover{Item20Text: Item20Text{V: "a"}, Slot: "s"})
	if err == nil {
		t.Fatal("embedded TextMarshaler silently replaced the outer struct")
	}
	// The value type itself must still marshal as text when it is a named field.
	n, err := gomap.ToTonyIR(&struct {
		T    Item20Text `tony:"field=t"`
		Slot string     `tony:"field=slot"`
	}{T: Item20Text{V: "a"}, Slot: "s"})
	if err != nil {
		t.Fatal(err)
	}
	tv, _ := n.GetPath("$.t")
	if tv == nil || tv.String != "T:a" {
		t.Errorf("named TextMarshaler field not dispatched: got %s", encode.MustString(n))
	}
}

// item20unexported is an embedded UNEXPORTED struct type. encoding/json promotes the
// exported fields of such a type; gomap used to drop them.
type item20unexported struct {
	B string `tony:"field=b"`
}

type item20UnexportedCover struct {
	item20unexported
	Slot string `tony:"field=slot"`
}

// TestUpstreamItem20_EmbeddedUnexportedTypePromoted guards comment 20's adjacent
// note: the embedded field's NAME is unexported, but its exported fields are
// readable and settable through it, so they belong on the wire.
func TestUpstreamItem20_EmbeddedUnexportedTypePromoted(t *testing.T) {
	n, err := gomap.ToTonyIR(&item20UnexportedCover{
		item20unexported: item20unexported{B: "y"},
		Slot:             "s",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := n.GetPath("$.b")
	if b == nil || b.String != "y" {
		t.Errorf("embedded unexported type's fields dropped: got %s", encode.MustString(n))
	}
	var got item20UnexportedCover
	if err := gomap.FromTonyIR(n, &got); err != nil {
		t.Fatal(err)
	}
	if got.B != "y" || got.Slot != "s" {
		t.Errorf("embedded unexported type did not round trip: %+v", got)
	}
}

// item21Embedded renames its field with a field= tag. Nothing here has a codec: this
// is plain embedding, which encode has always flattened under the renamed key.
type item21Embedded struct {
	System string `tony:"field=system"`
	Hidden string `tony:"omit"`
}

type item21Nested struct {
	item21Embedded
	Mid string `tony:"field=mid"`
}

type item21Host struct {
	item21Nested
	Slot []string `tony:"field=slot"`
}

// Item 21: the decode side must read an embedded field under its field= name. It
// registered the GO field name instead, so a renamed embedded field encoded as
// "system" and decoded only from "System" — plain embedding with renamed fields did
// not round trip at all, with no codec involved. Nested embedding and omit on an
// embedded field were dropped by the same branch.
func TestUpstreamItem21_EmbeddedFieldRenameRoundTrips(t *testing.T) {
	orig := &item21Host{
		item21Nested: item21Nested{
			item21Embedded: item21Embedded{System: "git", Hidden: "secret"},
			Mid:            "m",
		},
		Slot: []string{"o"},
	}
	node, err := gomap.ToTonyIR(orig)
	if err != nil {
		t.Fatal(err)
	}
	if hidden, _ := node.GetPath("$.Hidden"); hidden != nil {
		t.Errorf("omit ignored for an embedded field: got %s", encode.MustString(node))
	}
	var got item21Host
	if err := gomap.FromTonyIR(node, &got); err != nil {
		t.Fatal(err)
	}
	if got.System != "git" {
		t.Errorf("renamed embedded field not decoded: System = %q (doc %s)", got.System, encode.MustString(node))
	}
	if got.Mid != "m" {
		t.Errorf("field of a nested embedded struct not decoded: Mid = %q", got.Mid)
	}
	if got.Hidden != "" {
		t.Errorf("omit ignored on decode for an embedded field: Hidden = %q", got.Hidden)
	}
	if len(got.Slot) != 1 || got.Slot[0] != "o" {
		t.Errorf("outer field lost: Slot = %v", got.Slot)
	}
}

// item21Base is embedded by types that also declare a field under the same wire
// name, so the shadowing rule can be checked in both declaration orders.
type item21Base struct {
	Name string `tony:"field=name"`
}

type item21ShadowAfter struct {
	item21Base
	Name string `tony:"field=name"`
}

type item21ShadowBefore struct {
	Name string `tony:"field=name"`
	item21Base
}

// TestUpstreamItem21_OuterFieldShadowsEmbedded pins the rule the two paths now
// share: a field the type declares itself shadows one of the same name reached
// through embedding, whichever order they appear in, as in Go and encoding/json.
// Encode used to depend on declaration order — silently taking the outer field
// when the embedding came first, and refusing to encode at all when it came
// second, for a struct decode was content to shadow.
func TestUpstreamItem21_OuterFieldShadowsEmbedded(t *testing.T) {
	after, err := gomap.ToTonyIR(&item21ShadowAfter{item21Base: item21Base{Name: "emb"}, Name: "own"})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := after.GetPath("$.name"); n == nil || n.String != "own" {
		t.Errorf("embedded field not shadowed: got %s", encode.MustString(after))
	}

	before, err := gomap.ToTonyIR(&item21ShadowBefore{item21Base: item21Base{Name: "emb"}, Name: "own"})
	if err != nil {
		t.Fatalf("shadowing rejected when the outer field is declared first: %v", err)
	}
	if n, _ := before.GetPath("$.name"); n == nil || n.String != "own" {
		t.Errorf("embedded field not shadowed: got %s", encode.MustString(before))
	}

	var got item21ShadowBefore
	if err := gomap.FromTonyIR(before, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "own" || got.item21Base.Name != "" {
		t.Errorf("decode disagreed with encode about which field owns the key: %+v", got)
	}
}

// Item20Wrapper is the wrapper idiom: a struct written only to hang methods off a
// type from elsewhere, with nothing of its own to serialize.
type Item20Wrapper struct{ Item20Text }

type Item20WrapperHost struct {
	W Item20Wrapper `tony:"field=w"`
}

// TestUpstreamItem20_SoleEmbeddedWrapperDelegates guards the limit of the fix. An
// embedded type whose wire form is a scalar cannot be merged into a parent object,
// and refusing that is the point — but a type whose ONLY content is the embedded
// field has no parent object and nothing to lose, so it encodes and decodes as what
// it wraps, as it did before and as encoding/json does.
func TestUpstreamItem20_SoleEmbeddedWrapperDelegates(t *testing.T) {
	n, err := gomap.ToTonyIR(&Item20WrapperHost{W: Item20Wrapper{Item20Text{V: "a"}}})
	if err != nil {
		t.Fatalf("wrapper around a single embedded type rejected: %v", err)
	}
	w, _ := n.GetPath("$.w")
	if w == nil || w.String != "T:a" {
		t.Errorf("wrapper did not encode as what it wraps: got %s", encode.MustString(n))
	}
	var got Item20WrapperHost
	if err := gomap.FromTonyIR(n, &got); err != nil {
		t.Fatal(err)
	}
	if got.W.V != "T:a" {
		t.Errorf("wrapper did not decode through the embedded codec: %q", got.W.V)
	}
}
