// Package ident spells an element's identity as a path segment.
//
// One element has one name, the name is a field of the object that holds the element,
// and the index is a trie of those fields (element_identity.md). Everything here is a
// question about what strings may be such a field, and the answer is always the same
// shape: a NAME may, a QUERY may not.
//
// A name binds every field of a declared identity to a scalar. One binding renders
// (f=v) and several render <{f: v, g: w}>, fields sorted, values in the wire encoding
// with tag and comment stripped -- so (n=42) and (n="42") are different names, as they
// must be, the name being the only place the key's type survives.
//
// Nothing here consults a store, a schema or a document beyond the element it is handed.
// Which identity an array declares is the schema's to say; this package spells what it
// says.
package ident

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Binding is one field of an identity and the scalar it holds.
type Binding struct {
	Field string
	Value *ir.Node
}

// Name is an element's identity, spelled: every field of the identity bound, in field
// order.
type Name struct {
	bindings []Binding
}

// New makes a name from bindings. It refuses what cannot be a name: no bindings, a field
// bound twice, a value that is nil, null or not a scalar, and a field whose own spelling
// would break the name's punctuation.
//
// A null key is not a name: an element carrying one is unkeyed and cannot be addressed,
// which is the reading mergeop's keyed merge already takes of a document element that
// does not carry the key.
func New(bindings ...Binding) (Name, error) {
	if len(bindings) == 0 {
		return Name{}, errors.New("a name binds at least one field")
	}
	out := make([]Binding, 0, len(bindings))
	for _, b := range bindings {
		if strings.ContainsAny(b.Field, "=<>") || b.Field == "" {
			return Name{}, fmt.Errorf("%q cannot be an identity field: '=', '<' and '>' are the name's own punctuation", b.Field)
		}
		v := ir.Uncomment(b.Value)
		if v == nil || v.Type == ir.NullType {
			return Name{}, fmt.Errorf("identity field %q is null or missing, and a null key is not a name", b.Field)
		}
		switch v.Type {
		case ir.StringType, ir.NumberType, ir.BoolType:
		default:
			return Name{}, fmt.Errorf("identity field %q holds %s, and only a scalar can be spelled as a name", b.Field, v.Type)
		}
		for _, prev := range out {
			if prev.Field == b.Field {
				return Name{}, fmt.Errorf("identity field %q is bound twice", b.Field)
			}
		}
		out = append(out, Binding{Field: b.Field, Value: scalar(v)})
	}
	slices.SortFunc(out, func(a, b Binding) int { return strings.Compare(a.Field, b.Field) })
	return Name{bindings: out}, nil
}

// Of answers the name of elem under the identity fields, and false when elem has no
// name: a field missing, null, or not a scalar. A false answer is not an error -- the
// element is unkeyed -- and the caller decides what that means where it stands.
func Of(elem *ir.Node, fields []string) (Name, bool) {
	elem = ir.Uncomment(elem)
	if elem == nil || elem.Type != ir.ObjectType || len(fields) == 0 {
		return Name{}, false
	}
	bindings := make([]Binding, 0, len(fields))
	for _, f := range fields {
		v, err := elem.GetKPath(f)
		if err != nil || v == nil {
			return Name{}, false
		}
		bindings = append(bindings, Binding{Field: f, Value: v})
	}
	n, err := New(bindings...)
	if err != nil {
		return Name{}, false
	}
	return n, true
}

// Bindings answers the name's bindings, in field order. The values are the name's own
// copies, tag and comment stripped.
func (n Name) Bindings() []Binding { return slices.Clone(n.bindings) }

// Fields answers the identity the name binds, sorted.
func (n Name) Fields() []string {
	out := make([]string, len(n.bindings))
	for i, b := range n.bindings {
		out[i] = b.Field
	}
	return out
}

// Binds reports whether the name binds exactly the given identity -- every field and no
// other. A name that binds fewer, more, or different fields is a query against this
// identity, not a name under it.
func (n Name) Binds(fields []string) bool {
	if len(fields) != len(n.bindings) {
		return false
	}
	sorted := slices.Clone(fields)
	slices.Sort(sorted)
	for i, b := range n.bindings {
		if b.Field != sorted[i] {
			return false
		}
	}
	return true
}

// Field is the canonical spelling: the field name that holds the element in the stored
// object, and what the trie keys on. As a kpath segment it is quoted by kpath's own rule,
// since it always holds punctuation kpath quotes; that is kpath.ChildField's business,
// not this one's.
func (n Name) Field() string {
	if len(n.bindings) == 1 {
		return "(" + n.bindings[0].Field + "=" + wire(n.bindings[0].Value) + ")"
	}
	var b strings.Builder
	b.WriteString("<{")
	for i, bd := range n.bindings {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(bd.Field)
		b.WriteString(": ")
		b.WriteString(wire(bd.Value))
	}
	b.WriteString("}>")
	return b.String()
}

// String is Field.
func (n Name) String() string { return n.Field() }

// Stamp puts the name's bindings into elem where they are missing, and reports the first
// field where elem disagrees with the name. The NAME is authoritative: an element whose
// key fields say something else is invalid, and is refused where it is written.
func (n Name) Stamp(elem *ir.Node) error {
	elem = ir.Uncomment(elem)
	if elem == nil || elem.Type != ir.ObjectType {
		return fmt.Errorf("an element named %s must be an object", n.Field())
	}
	for _, b := range n.bindings {
		have, err := elem.GetKPath(b.Field)
		switch {
		case err != nil || have == nil || ir.Uncomment(have) == nil:
			if strings.Contains(b.Field, ".") {
				return fmt.Errorf("element %s does not carry %q, and a nested identity field cannot be filled in", n.Field(), b.Field)
			}
			elem.Fields = append(elem.Fields, ir.FromString(b.Field))
			elem.Values = append(elem.Values, b.Value.Clone())
		default:
			if !scalar(ir.Uncomment(have)).DeepEqual(b.Value) {
				return fmt.Errorf("element %s carries %s=%s, and its name is authoritative",
					n.Field(), b.Field, wire(ir.Uncomment(have)))
			}
		}
	}
	return nil
}

// Parse reads a field string as a name. ok is false for a field that is not spelled as
// one -- an ordinary field name -- and err is set for one that is spelled as a name and
// does not read as one, which is a client's mistake worth naming.
func Parse(field string) (n Name, ok bool, err error) {
	switch {
	case strings.HasPrefix(field, "(") && strings.HasSuffix(field, ")") && len(field) > 2:
		inner := field[1 : len(field)-1]
		eq := strings.IndexByte(inner, '=')
		if eq <= 0 {
			return Name{}, true, fmt.Errorf("%q is not a name: a binding is (field=value)", field)
		}
		v, err := parseValue(inner[eq+1:])
		if err != nil {
			return Name{}, true, fmt.Errorf("%q is not a name: %w", field, err)
		}
		n, err := New(Binding{Field: inner[:eq], Value: v})
		if err != nil {
			return Name{}, true, fmt.Errorf("%q is not a name: %w", field, err)
		}
		return n, true, nil
	case strings.HasPrefix(field, "<{") && strings.HasSuffix(field, "}>") && len(field) > 4:
		obj, err := parseValue(field[1 : len(field)-1])
		if err != nil {
			return Name{}, true, fmt.Errorf("%q is not a name: %w", field, err)
		}
		if obj.Type != ir.ObjectType {
			return Name{}, true, fmt.Errorf("%q is not a name: <{...}> holds bindings", field)
		}
		bindings := make([]Binding, 0, len(obj.Fields))
		for i, f := range obj.Fields {
			if i >= len(obj.Values) {
				break
			}
			bindings = append(bindings, Binding{Field: f.String, Value: obj.Values[i]})
		}
		n, err := New(bindings...)
		if err != nil {
			return Name{}, true, fmt.Errorf("%q is not a name: %w", field, err)
		}
		return n, true, nil
	}
	return Name{}, false, nil
}

// IsName reports whether field is spelled as a name, well formed or not.
func IsName(field string) bool {
	_, ok, _ := Parse(field)
	return ok
}

// scalar is a copy of a scalar with the tag and comment off it: the name is the value and
// nothing said about the value.
func scalar(v *ir.Node) *ir.Node {
	out := ir.Uncomment(v).Clone()
	out.Tag = ""
	return out
}

// wire renders a scalar the way the name spells it.
func wire(v *ir.Node) string {
	var buf bytes.Buffer
	if err := encode.Encode(v, &buf, encode.EncodeWire(true)); err != nil {
		return "?"
	}
	return strings.TrimSpace(buf.String())
}

func parseValue(text string) (*ir.Node, error) {
	n, err := parse.Parse([]byte(text))
	if err != nil {
		return nil, err
	}
	n = ir.Uncomment(n)
	if n == nil {
		return nil, errors.New("empty value")
	}
	n.Tag = ""
	return n, nil
}

// CanonicalPath spells a client's path the way the store keeps it. Three spellings name
// one element -- items."(sku=A)" is the stored field, items(sku=A) is sugar that says the
// same thing, and items(A) is sugar resolved against the schema -- and the two sugars
// canonicalize to the first here, at the boundary, so nothing inside the store needs a
// (key) segment or a schema to read a path back.
//
// A field spelled as a name under a keyed array is checked against the identity: a
// binding of some other field is a query, not a place, and is refused where a place is
// needed. A (key) segment anywhere but under a keyed array is refused too; the store has
// no element for it to name.
func CanonicalPath(schema *api.Schema, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	kp, err := kpath.Parse(path)
	if err != nil {
		return "", err
	}
	// A wildcard names a SET, and nothing in it is an element with a name to spell: the
	// path is answered as it came, and the read says what it says about wildcards.
	for x := kp; x != nil; x = x.Next {
		if x.Wild() {
			return path, nil
		}
	}
	out := ""      // the canonical path so far
	schemaAt := "" // the schema's path for the same point, names elided
	inElement := false
	for x := kp; x != nil; x = x.Next {
		switch {
		case x.Field != nil:
			// Inside an element, a field is the element's own and extends the schema's
			// path: elements inherit the array's path, so items."(sku=A)".q is the
			// schema's items.q.
			if inElement {
				inElement = false
				out = kpath.ChildField(out, *x.Field)
				schemaAt = kpath.ChildField(schemaAt, *x.Field)
				continue
			}
			if identity := schema.Identity(schemaAt); len(identity) > 0 {
				name, isName, err := Parse(*x.Field)
				if err != nil {
					return "", fmt.Errorf("%q: %w", path, err)
				}
				if !isName {
					return "", fmt.Errorf("%q: %q is not a name, and %s is keyed: an element is addressed "+
						"by its identity, as %s", path, *x.Field, place(schemaAt), example(identity))
				}
				if !name.Binds(identity) {
					return "", fmt.Errorf("%q: %s does not name an element of %s, whose identity is %s",
						path, name, place(schemaAt), strings.Join(identity, ","))
				}
				out = kpath.ChildField(out, name.Field())
				inElement = true
				continue
			}
			out = kpath.ChildField(out, *x.Field)
			schemaAt = kpath.ChildField(schemaAt, *x.Field)
		case x.Key != nil:
			identity := schema.Identity(schemaAt)
			if len(identity) == 0 {
				return "", fmt.Errorf("%q: %s has no identity, so (%s) names no element of it", path, place(schemaAt), *x.Key)
			}
			name, err := nameFromKey(*x.Key, identity)
			if err != nil {
				return "", fmt.Errorf("%q: %w", path, err)
			}
			out = kpath.ChildField(out, name.Field())
			inElement = true
		case x.Index != nil:
			if schema.Keyed(schemaAt) {
				return "", fmt.Errorf("%q: %s is keyed, and identity replaces position: [%d] names nothing there",
					path, place(schemaAt), *x.Index)
			}
			out += x.SegmentString()
			schemaAt += x.SegmentString()
		default:
			out += x.SegmentString()
			schemaAt += x.SegmentString()
		}
	}
	return out, nil
}

// nameFromKey reads the text of a (key) segment as a name under identity: bindings
// written f=v and separated by commas, or a bare value for a one-field identity.
//
// kpath hands the text over with the segment's own quotes removed, so a bare value that
// is spelled "42" arrives as 42 and reads as a number. A string that would read as a
// number is named through the binding form, (sku="42"), whose inner quotes survive.
func nameFromKey(text string, identity []string) (Name, error) {
	if !strings.Contains(text, "=") {
		if len(identity) != 1 {
			return Name{}, fmt.Errorf("(%s) binds no field, and the identity here is %s", text, strings.Join(identity, ","))
		}
		v, err := parseValue(text)
		if err != nil {
			return Name{}, fmt.Errorf("(%s): %w", text, err)
		}
		n, err := New(Binding{Field: identity[0], Value: v})
		if err != nil {
			return Name{}, fmt.Errorf("(%s): %w", text, err)
		}
		return n, nil
	}
	var bindings []Binding
	for _, part := range strings.Split(text, ",") {
		eq := strings.IndexByte(part, '=')
		if eq <= 0 {
			return Name{}, fmt.Errorf("(%s): %q is not a binding", text, part)
		}
		v, err := parseValue(strings.TrimSpace(part[eq+1:]))
		if err != nil {
			return Name{}, fmt.Errorf("(%s): %w", text, err)
		}
		bindings = append(bindings, Binding{Field: strings.TrimSpace(part[:eq]), Value: v})
	}
	n, err := New(bindings...)
	if err != nil {
		return Name{}, fmt.Errorf("(%s): %w", text, err)
	}
	if !n.Binds(identity) {
		return Name{}, fmt.Errorf("(%s) does not bind the identity %s", text, strings.Join(identity, ","))
	}
	return n, nil
}

func place(schemaAt string) string {
	if schemaAt == "" {
		return "the document root"
	}
	return schemaAt
}

func example(identity []string) string {
	if len(identity) == 1 {
		return "(" + identity[0] + "=<value>)"
	}
	parts := make([]string, len(identity))
	for i, f := range identity {
		parts[i] = f + ": <value>"
	}
	return "<{" + strings.Join(parts, ", ") + "}>"
}
