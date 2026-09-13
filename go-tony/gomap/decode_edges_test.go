package gomap

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// ownCodec has a codec of its own, as a generated type does.
type ownCodec struct{ V int }

func (c *ownCodec) FromTonyIR(node *ir.Node, opts ...UnmapOption) error {
	if node.Type == ir.NullType {
		*c = ownCodec{}
		return nil
	}
	c.V = int(*node.Int64)
	return nil
}

// Three edges of the reflection decoder (p478tacqh12krg32msn0 item 26): a nil
// pointer to a type with its own codec is allocated before the codec is asked; a
// head comment at depth two or more decodes into an interface as its value; a
// head-commented *time.Time reads the text under the comment.
func TestDecodeEdges(t *testing.T) {
	var p *ownCodec
	if err := FromTonyIR(ir.FromInt(3), &p); err != nil || p == nil || p.V != 3 {
		t.Errorf("nil pointer to a codec type: p=%v err=%v, want V=3", p, err)
	}
	var q *ownCodec
	if err := FromTonyIR(ir.Null(), &q); err != nil || q != nil {
		t.Errorf("null into a nil pointer: q=%v err=%v, want it left nil", q, err)
	}

	doc, err := parse.Parse([]byte("a:\n  b:\n    # deep\n    1\n"), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := FromTonyIR(doc, &v); err != nil {
		t.Fatalf("comment at depth two into any: %v", err)
	}
	if got := v.(map[string]any)["a"].(map[string]any)["b"]; got != int64(1) {
		t.Errorf("a.b = %v (%T), want 1", got, got)
	}

	type host struct {
		T *time.Time `tony:"field=t"`
	}
	doc, err = parse.Parse([]byte("t:\n  # when\n  \"2024-01-02T03:04:05Z\"\n"), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	var h host
	if err := FromTonyIR(doc, &h); err != nil {
		t.Fatalf("head-commented *time.Time: %v", err)
	}
	if h.T == nil || h.T.Year() != 2024 || h.T.Second() != 5 {
		t.Errorf("T = %v, want 2024-01-02T03:04:05Z", h.T)
	}
}
