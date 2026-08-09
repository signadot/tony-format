package tony

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A diff is only worth anything if applying it arrives where it said it would, and that
// is not something reading one tells you: every defect found in the keyed-list diff
// produced output that ENCODED correctly and differed only in a tag, in how a key was
// spelled, or in a field the merge quietly dropped. So this generates documents,
// mutates them, and checks the pair against the operations rather than against an
// expectation written by hand.
//
// Documents are generated as TEXT and parsed, so the shapes come through the same route
// a real one does -- a node tree built directly in Go can hold combinations the parser
// never produces, and would test something narrower than what callers have.
//
// Cases are seeded by index, so a failure names a seed that reproduces it exactly.

// genConfig bounds a generated document. Small: the point is coverage of SHAPES --
// keyed and plain, nested, tagged, absent -- not of size.
type genConfig struct {
	maxDepth  int
	maxFields int
	maxElems  int
}

var defaultGen = genConfig{maxDepth: 3, maxFields: 4, maxElems: 4}

// gnode is the generator's own model. Rendering it is easier to keep correct than
// assembling ir nodes, and mutating it is easier than mutating a parsed tree.
type gnode struct {
	kind    string // "scalar" | "object" | "array" | "keyed"
	scalar  string // rendered scalar text, for kind == "scalar"
	fields  []gfield
	elems   []*gnode
	keyPath string // for kind == "keyed": "name" or "meta.name"
	tag     string // a DATA tag, never an op -- see nonOpTags
}

type gfield struct {
	name string
	val  *gnode
}

var (
	fieldNames = []string{"a", "b", "c", "d", "v", "rev"}
	// Keys chosen so some MUST stay quoted (digit-first, spaced) -- the diff used to
	// rebuild keys by re-parsing their rendering, which silently unquoted them.
	keyNames = []string{"a", "b", "c", "9a", "x y", "zz"}
	scalars  = []string{`1`, `2`, `-7`, `0`, `true`, `false`, `null`, `"s"`, `"other"`, `"9a"`, `""`}
	// Tags that are NOT registered merge ops, so they are carried as data rather than
	// executed. !tag and !type are real ops; these are not.
	nonOpTags = []string{"", "", "", "!t1", "!t2"}
)

type generator struct {
	r   *rand.Rand
	cfg genConfig
}

func (g *generator) pick(ss []string) string { return ss[g.r.Intn(len(ss))] }

func (g *generator) node(depth int) *gnode {
	if depth <= 0 {
		return &gnode{kind: "scalar", scalar: g.pick(scalars), tag: g.pick(nonOpTags)}
	}
	switch g.r.Intn(10) {
	case 0, 1, 2:
		return &gnode{kind: "scalar", scalar: g.pick(scalars), tag: g.pick(nonOpTags)}
	case 3, 4, 5:
		return g.object(depth)
	case 6, 7:
		return g.array(depth)
	default:
		return g.keyed(depth)
	}
}

func (g *generator) object(depth int) *gnode {
	n := &gnode{kind: "object", tag: g.pick(nonOpTags)}
	used := map[string]bool{}
	for i := g.r.Intn(g.cfg.maxFields) + 1; i > 0; i-- {
		name := g.pick(fieldNames)
		if used[name] {
			continue
		}
		used[name] = true
		n.fields = append(n.fields, gfield{name: name, val: g.node(depth - 1)})
	}
	return n
}

func (g *generator) array(depth int) *gnode {
	n := &gnode{kind: "array", tag: g.pick(nonOpTags)}
	for i := g.r.Intn(g.cfg.maxElems) + 1; i > 0; i-- {
		n.elems = append(n.elems, g.node(depth-1))
	}
	return n
}

// keyed builds a !key list. Element keys are unique: two elements under one key is not
// a state a keyed list can hold (the merge collapses them), so generating it would test
// a shape that cannot exist rather than one that can.
func (g *generator) keyed(depth int) *gnode {
	nested := g.r.Intn(2) == 0
	keyPath := "name"
	if nested {
		keyPath = "meta.name"
	}
	n := &gnode{kind: "keyed", keyPath: keyPath, tag: g.pick(nonOpTags)}
	used := map[string]bool{}
	for i := g.r.Intn(g.cfg.maxElems) + 1; i > 0; i-- {
		k := g.pick(keyNames)
		if used[k] {
			continue
		}
		used[k] = true
		n.elems = append(n.elems, g.keyedElem(k, keyPath, depth))
	}
	if len(n.elems) == 0 {
		n.elems = append(n.elems, g.keyedElem(g.pick(keyNames), keyPath, depth))
	}
	return n
}

func (g *generator) keyedElem(key, keyPath string, depth int) *gnode {
	elem := &gnode{kind: "object"}
	if keyPath == "name" {
		elem.fields = append(elem.fields, gfield{name: "name", val: strNode(key)})
	} else {
		elem.fields = append(elem.fields, gfield{name: "meta", val: &gnode{
			kind:   "object",
			fields: []gfield{{name: "name", val: strNode(key)}},
		}})
	}
	for i := g.r.Intn(2) + 1; i > 0; i-- {
		elem.fields = append(elem.fields, gfield{name: g.pick(fieldNames), val: g.node(depth - 2)})
	}
	return dedupFields(elem)
}

func strNode(s string) *gnode {
	return &gnode{kind: "scalar", scalar: fmt.Sprintf("%q", s)}
}

func dedupFields(n *gnode) *gnode {
	seen := map[string]bool{}
	out := n.fields[:0]
	for _, f := range n.fields {
		if seen[f.name] {
			continue
		}
		seen[f.name] = true
		out = append(out, f)
	}
	n.fields = out
	return n
}

func (n *gnode) clone() *gnode {
	c := *n
	c.fields = nil
	for _, f := range n.fields {
		c.fields = append(c.fields, gfield{name: f.name, val: f.val.clone()})
	}
	c.elems = nil
	for _, e := range n.elems {
		c.elems = append(c.elems, e.clone())
	}
	return &c
}

// mutate returns a changed copy. The changes are the ones a diff has to describe:
// a value replaced, a field or element appearing or disappearing, a tag changing, and
// -- for a keyed list -- elements moving, which is NOT a change to what the list holds.
func (g *generator) mutate(n *gnode) *gnode {
	c := n.clone()
	switch c.kind {
	case "scalar":
		c.scalar = g.pick(scalars)
		if g.r.Intn(4) == 0 {
			c.tag = g.pick(nonOpTags)
		}
	case "object":
		switch g.r.Intn(4) {
		case 0: // change one field's value
			if len(c.fields) > 0 {
				i := g.r.Intn(len(c.fields))
				c.fields[i].val = g.mutate(c.fields[i].val)
			}
		case 1: // add a field
			c.fields = append(c.fields, gfield{name: g.pick(fieldNames), val: g.node(1)})
			dedupFields(c)
		case 2: // drop a field
			if len(c.fields) > 1 {
				i := g.r.Intn(len(c.fields))
				c.fields = append(c.fields[:i], c.fields[i+1:]...)
			}
		default: // replace wholesale
			return g.node(2)
		}
	case "array":
		switch g.r.Intn(4) {
		case 0:
			if len(c.elems) > 0 {
				i := g.r.Intn(len(c.elems))
				c.elems[i] = g.mutate(c.elems[i])
			}
		case 1:
			c.elems = append(c.elems, g.node(1))
		case 2:
			if len(c.elems) > 1 {
				i := g.r.Intn(len(c.elems))
				c.elems = append(c.elems[:i], c.elems[i+1:]...)
			}
		default:
			return g.node(2)
		}
	case "keyed":
		switch g.r.Intn(5) {
		case 0: // change a non-key field of one element
			if len(c.elems) > 0 {
				e := c.elems[g.r.Intn(len(c.elems))]
				if len(e.fields) > 1 {
					i := 1 + g.r.Intn(len(e.fields)-1)
					e.fields[i].val = g.mutate(e.fields[i].val)
				}
			}
		case 1: // add an element under a key not already present
			used := map[string]bool{}
			for _, e := range c.elems {
				used[keyOf(e, c.keyPath)] = true
			}
			for _, k := range keyNames {
				if !used[k] {
					c.elems = append(c.elems, g.keyedElem(k, c.keyPath, 3))
					break
				}
			}
		case 2: // remove an element
			if len(c.elems) > 1 {
				i := g.r.Intn(len(c.elems))
				c.elems = append(c.elems[:i], c.elems[i+1:]...)
			}
		case 3: // reorder: identity-keyed, so this changes nothing about the data
			g.r.Shuffle(len(c.elems), func(i, j int) { c.elems[i], c.elems[j] = c.elems[j], c.elems[i] })
		default: // empty it
			c.elems = c.elems[:1]
		}
	}
	return c
}

func keyOf(elem *gnode, keyPath string) string {
	target := elem
	if keyPath == "meta.name" {
		for _, f := range elem.fields {
			if f.name == "meta" {
				target = f.val
				break
			}
		}
	}
	for _, f := range target.fields {
		if f.name == "name" {
			return strings.Trim(f.val.scalar, `"`)
		}
	}
	return ""
}

func (n *gnode) render() string {
	var b strings.Builder
	n.renderTo(&b)
	return b.String()
}

func (n *gnode) renderTo(b *strings.Builder) {
	if n.tag != "" {
		b.WriteString(n.tag)
		b.WriteByte(' ')
	}
	switch n.kind {
	case "scalar":
		b.WriteString(n.scalar)
	case "object":
		b.WriteByte('{')
		for i, f := range n.fields {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "%q: ", f.name)
			f.val.renderTo(b)
		}
		b.WriteByte('}')
	case "array", "keyed":
		if n.kind == "keyed" {
			fmt.Fprintf(b, "!key(%s) ", n.keyPath)
		}
		b.WriteByte('[')
		for i, e := range n.elems {
			if i > 0 {
				b.WriteString(", ")
			}
			e.renderTo(b)
		}
		b.WriteByte(']')
	}
}

// repeatedTagLabel finds a node whose composed tag names the same label twice, which no
// tag should: the labels are a set, and a repeat means something added one that was
// already there.
func repeatedTagLabel(n *ir.Node) (string, bool) {
	if n == nil {
		return "", false
	}
	seen := map[string]bool{}
	for tag := n.Tag; tag != ""; {
		head, _, rest := ir.TagArgs(tag)
		if head == "" {
			break
		}
		if seen[head] {
			return head, true
		}
		seen[head] = true
		tag = rest
	}
	for _, f := range n.Fields {
		if l, ok := repeatedTagLabel(f); ok {
			return l, true
		}
	}
	for _, v := range n.Values {
		if l, ok := repeatedTagLabel(v); ok {
			return l, true
		}
	}
	return "", false
}

// TestDiffPatchRoundTripProperty is the property itself: for generated a and b,
// Patch(a, Diff(a, b)) reproduces b.
//
// Comparison ignores presentation (see leftover): !bracket and friends record how a
// value was written, and patching drops them by design, so a difference there is not
// the diff failing to carry the data.
func TestDiffPatchRoundTripProperty(t *testing.T) {
	const cases = 500
	failures := 0
	for i := range cases {
		seed := int64(i) + 1
		g := &generator{r: rand.New(rand.NewSource(seed)), cfg: defaultGen}

		aModel := g.node(g.cfg.maxDepth)
		bModel := g.mutate(aModel)
		aText, bText := aModel.render(), bModel.render()

		a, err := parse.Parse([]byte(aText))
		if err != nil {
			t.Fatalf("seed %d: generated unparseable document %s: %v", seed, aText, err)
		}
		b, err := parse.Parse([]byte(bText))
		if err != nil {
			t.Fatalf("seed %d: generated unparseable document %s: %v", seed, bText, err)
		}

		// Diff and Patch may re-parent the nodes they are given (ir.FromMap sets Parent
		// on the values it collects), so they get copies and the originals stay as
		// parsed for the comparison.
		d := Diff(a.Clone(), b.Clone())
		if d == nil {
			// No diff claimed: then the two must already agree.
			if left := leftover(a, b); left != nil {
				t.Errorf("seed %d: Diff reported no difference but one remains\n a: %s\n b: %s\n left: %s",
					seed, aText, bText, encode.MustString(left))
				failures++
			}
			continue
		}

		got, err := Patch(a.Clone(), d.Clone())
		if err != nil {
			t.Errorf("seed %d: Patch failed\n a: %s\n b: %s\n diff: %s\n err: %v",
				seed, aText, bText, encode.MustString(d), err)
			failures++
			continue
		}
		if got == nil {
			t.Errorf("seed %d: Patch resolved the document away\n a: %s\n b: %s\n diff: %s",
				seed, aText, bText, encode.MustString(d))
			failures++
			continue
		}
		if left := leftover(got, b); left != nil {
			t.Errorf("seed %d: round trip did not arrive at b\n a: %s\n b: %s\n diff: %s\n left: %s",
				seed, aText, bText, encode.MustString(d), encode.MustString(left))
			failures++
		}
		// The comparison above ignores presentation, which leaves it blind to a tag
		// being applied twice -- the shape of one of the defects this was written
		// after (!bracket.bracket.key(name), from a tag restored onto a result that
		// already carried it). A composed tag repeating a label is meaningless whatever
		// the label is, so check that separately rather than widen the comparison into
		// asserting a fidelity patching does not promise.
		if label, ok := repeatedTagLabel(got); ok {
			t.Errorf("seed %d: patched result has %q twice in one tag\n a: %s\n b: %s\n diff: %s\n got: %s",
				seed, label, aText, bText, encode.MustString(d), encode.MustString(got))
			failures++
		}
		if failures > 10 {
			t.Fatalf("stopping after %d failures", failures)
		}
	}
}

// TestDiffSelfIsEmptyProperty: a document never differs from itself. This is what the
// keyed branch got wrong in the other direction -- it panicked on exactly this case,
// since two equal lists are the ordinary input to a diff, not a corner one.
func TestDiffSelfIsEmptyProperty(t *testing.T) {
	const cases = 500
	for i := range cases {
		seed := int64(i) + 1
		g := &generator{r: rand.New(rand.NewSource(seed)), cfg: defaultGen}
		text := g.node(g.cfg.maxDepth).render()
		n, err := parse.Parse([]byte(text))
		if err != nil {
			t.Fatalf("seed %d: generated unparseable document %s: %v", seed, text, err)
		}
		if d := Diff(n.Clone(), n.Clone()); d != nil {
			t.Errorf("seed %d: document differs from itself\n doc: %s\n diff: %s",
				seed, text, encode.MustString(d))
		}
	}
}
