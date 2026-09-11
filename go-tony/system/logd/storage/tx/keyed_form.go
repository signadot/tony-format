package tx

import (
	"fmt"
	"slices"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/ident"
)

// LowerKeyed rewrites every keyed array in a write into the form the store keeps.
//
// Once identity no longer rests on position, nothing is left that makes a keyed array an
// array in the store, so it is not stored as one: it is an OBJECT whose field names are
// the elements' names, and array-ness is presentation, put back by the read
// (element_identity.md). What this buys is that the name precedes the content because it
// IS the field name, so a stream knows an element's identity before it sees a byte of
// it; the substrate needs no schema, since CurrentPath answers a field path, snap indexes
// fields and the trie keys on segments; and there is no merge at the container -- a
// delta names an element, is recorded at that name and folded there, and the other N are
// not read, not rewritten and not resident.
//
// The schema is the authority on what keys an array, and lowering is where it is applied:
// the last place that has both the schema and the whole element. So this is also where
// the rules that keep a name a name are enforced, at the write, where a refusal is still a
// client error rather than a wrong answer later:
//
//	a !key on an array the schema does not key is refused -- two names for one element
//	is the one thing the trie cannot hold;
//	an element without a name (a null or missing identity field) is refused;
//	an element whose identity fields disagree with its name is refused, the name being
//	authoritative; one that lacks them has them filled in from the name;
//	a positional operation on a keyed array -- !arraydiff, an index -- is refused, since
//	identity REPLACES position for that array.
//
// Paths follow InjectAutoIDs: each patcher's data starts at its own API.Path, object
// fields extend it, and the elements of a keyed array inherit the array's path -- which is
// what api.AutoIDField.Path ("orders.items") means by it. An element's own name is not a
// step in the schema's path; a write rooted at items."(sku=A)" is a write to an element of
// items, and is stamped with that name.
func LowerKeyed(schema *api.Schema, data []*PatcherData) error {
	for _, pd := range data {
		if pd.API == nil || pd.API.Data == nil {
			continue
		}
		at, err := schemaPathOf(schema, pd.API.Path)
		if err != nil {
			return err
		}
		if at.element != nil {
			if err := stampElement(pd.API.Data, *at.element); err != nil {
				return fmt.Errorf("write at %q: %w", pd.API.Path, err)
			}
		}
		if err := lowerKeyedRec(schema, pd.API.Data, at.path, at.element != nil, pd.API.Path); err != nil {
			return err
		}
	}
	return nil
}

// LowerMatches lowers every precondition the way LowerKeyed lowers a write, so a pattern
// written in the client's vocabulary -- a keyed array, an element under its name -- is
// evaluated against the form the store holds, which is the form CommitOps.StateAt answers in.
// A precondition that cannot be spelled in that form is refused as a write would be.
func LowerMatches(schema *api.Schema, data []*PatcherData) error {
	for _, pd := range data {
		if pd.API == nil || pd.API.Match == nil || pd.API.Match.Data == nil {
			continue
		}
		at, err := schemaPathOf(schema, pd.API.Match.Path)
		if err != nil {
			return err
		}
		if at.element != nil {
			if err := stampElement(pd.API.Match.Data, *at.element); err != nil {
				return fmt.Errorf("precondition at %q: %w", pd.API.Match.Path, err)
			}
		}
		if err := lowerKeyedRec(schema, pd.API.Match.Data, at.path, at.element != nil, pd.API.Match.Path); err != nil {
			return err
		}
	}
	return nil
}

// schemaAddress is where a write's data sits in the schema's terms: the array or field
// path the schema declares things at, with element names elided, and the name of the
// element when the data IS an element -- when the write's own path ends at a name.
type schemaAddress struct {
	path    string
	element *ident.Name
}

// schemaPathOf translates a document path into the path the schema declares things at.
// A name under a keyed array is not a step: items."(sku=A)".qty is the schema's items.qty,
// and items."(sku=A)" is an element of the schema's items.
func schemaPathOf(schema *api.Schema, path string) (schemaAddress, error) {
	kp, err := kpath.Parse(path)
	if err != nil {
		return schemaAddress{}, fmt.Errorf("path %q: %w", path, err)
	}
	acc := ""
	var elem *ident.Name
	for x := kp; x != nil; x = x.Next {
		inElement := elem != nil
		elem = nil
		switch {
		case x.Field != nil:
			// Inside an element, a field is the element's own and extends the schema's
			// path: elements inherit the array's path.
			if inElement {
				acc = kpath.ChildField(acc, *x.Field)
				continue
			}
			if schema.Keyed(acc) {
				name, isName, err := ident.Parse(*x.Field)
				if err != nil {
					return schemaAddress{}, fmt.Errorf("path %q: %w", path, err)
				}
				if isName {
					if !name.Binds(schema.Identity(acc)) {
						return schemaAddress{}, fmt.Errorf("path %q: %s does not name an element of %s, "+
							"whose identity is %s", path, name, pathOrRoot(acc), strings.Join(schema.Identity(acc), ","))
					}
					elem = &name
					continue
				}
				return schemaAddress{}, fmt.Errorf("path %q: %q is not a name, and %s is keyed: an element "+
					"is addressed by its identity, as %s", path, *x.Field, pathOrRoot(acc), exampleName(schema.Identity(acc)))
			}
			acc = kpath.ChildField(acc, *x.Field)
		case x.Key != nil:
			return schemaAddress{}, fmt.Errorf("path %q: a (key) segment names an element by value where "+
				"the store names it by identity; address it as %s", path, exampleName(schema.Identity(acc)))
		case x.Index != nil:
			if schema.Keyed(acc) {
				return schemaAddress{}, fmt.Errorf("path %q: %s is keyed, and identity replaces position: "+
					"[%d] names nothing there", path, pathOrRoot(acc), *x.Index)
			}
			acc += x.SegmentString()
		default:
			acc += x.SegmentString()
		}
	}
	return schemaAddress{path: acc, element: elem}, nil
}

func exampleName(identity []string) string {
	if len(identity) == 1 {
		return "(" + identity[0] + "=<value>)"
	}
	parts := make([]string, len(identity))
	for i, f := range identity {
		parts[i] = f + ": <value>"
	}
	return "<{" + strings.Join(parts, ", ") + "}>"
}

// lowerKeyedRec walks a patch node in place. at is the schema path the node sits at;
// atElement says the node is an ELEMENT of the keyed array at `at` rather than the value
// at `at` itself. where is the client's own path, for errors.
func lowerKeyedRec(schema *api.Schema, n *ir.Node, at string, atElement bool, where string) error {
	n = ir.Uncomment(n)
	if n == nil {
		return nil
	}
	// A !raw subtree is a document the store CARRIES rather than one it owns: the escape
	// says to treat it as data, interpreting no operation at any depth, and it lands with
	// its own tags intact. Reshaping it is the store editing someone else's document
	// (5hmq80f3h12krh1mbsn0).
	if ir.TagHas(n.Tag, libdiff.RawTag) {
		return nil
	}
	op := opOf(n)
	identity := schema.Identity(at)
	if len(identity) > 0 && !atElement {
		return lowerKeyedArray(schema, n, op, at, identity, where)
	}
	switch n.Type {
	case ir.ArrayType:
		if _, keyed := n.KeyField(); keyed || op == "key" {
			return fmt.Errorf("write at %q: the array at %s carries !key, and the schema gives it no "+
				"identity; a patch does not declare one, the schema does", where, pathOrRoot(at))
		}
		for i, elem := range n.Values {
			if err := lowerKeyedRec(schema, elem, fmt.Sprintf("%s[%d]", at, i), false, where); err != nil {
				return err
			}
		}
	case ir.ObjectType:
		if op == "replace" {
			// The operands are documents at this path, and are lowered as such.
			for _, side := range []string{"from", "to"} {
				if v := ir.Get(n, side); v != nil {
					if err := lowerKeyedRec(schema, v, at, atElement, where); err != nil {
						return err
					}
				}
			}
			return nil
		}
		for i, field := range n.Fields {
			if i >= len(n.Values) {
				break
			}
			if err := lowerKeyedRec(schema, n.Values[i], kpath.ChildField(at, field.String), false, where); err != nil {
				return err
			}
		}
	}
	return nil
}

// lowerKeyedArray is the node at a keyed array's own path: an array the client wrote, or
// the object form already -- a write rooted at an element arrives as one, from the path.
func lowerKeyedArray(schema *api.Schema, n *ir.Node, op, at string, identity []string, where string) error {
	switch op {
	case "replace":
		for _, side := range []string{"from", "to"} {
			if v := ir.Get(n, side); v != nil {
				if err := lowerKeyedRec(schema, v, at, false, where); err != nil {
					return err
				}
			}
		}
		return nil
	case "arraydiff":
		return fmt.Errorf("write at %q: %s is keyed, and identity replaces position: an !arraydiff "+
			"names elements by where they are", where, pathOrRoot(at))
	case "", "key", "insert", "delete":
	default:
		return fmt.Errorf("write at %q: !%s cannot be applied to the keyed array at %s; write its "+
			"elements by name", where, op, pathOrRoot(at))
	}
	switch n.Type {
	case ir.ArrayType:
		if have, keyed := n.KeyField(); keyed && have != "" {
			if len(identity) != 1 || have != identity[0] {
				return fmt.Errorf("write at %q: %s is declared keyed by %s but the patch keys it by %q; "+
					"one array has one identity", where, pathOrRoot(at), strings.Join(identity, ","), have)
			}
		}
		fields := make([]*ir.Node, 0, len(n.Values))
		values := make([]*ir.Node, 0, len(n.Values))
		seen := map[string]bool{}
		for i, elem := range n.Values {
			carrier := keyCarrier(elem)
			name, ok := ident.Of(carrier, identity)
			if !ok {
				return fmt.Errorf("write at %q: element %d of %s has no name: its identity %s is missing, "+
					"null, or not a scalar", where, i, pathOrRoot(at), strings.Join(identity, ","))
			}
			if seen[name.Field()] {
				return fmt.Errorf("write at %q: %s names two elements of %s", where, name, pathOrRoot(at))
			}
			seen[name.Field()] = true
			if err := stampElement(elem, name); err != nil {
				return fmt.Errorf("write at %q: %w", where, err)
			}
			if err := lowerKeyedRec(schema, elem, at, true, where); err != nil {
				return err
			}
			fields = append(fields, ir.FromString(name.Field()))
			values = append(values, elem)
		}
		sortByName(fields, values)
		n.Type = ir.ObjectType
		n.Fields = fields
		n.Values = values
		n.Tag = ir.TagRemove(ir.StripPresentation(n.Tag), ir.KeyTag)
		for i, v := range n.Values {
			v.Parent, v.ParentIndex = n, i
		}
		return nil
	case ir.ObjectType:
		for i, field := range n.Fields {
			if i >= len(n.Values) {
				break
			}
			name, isName, err := ident.Parse(field.String)
			if err != nil {
				return fmt.Errorf("write at %q: %w", where, err)
			}
			if !isName {
				return fmt.Errorf("write at %q: %q is not a name, and %s is keyed: its elements are "+
					"addressed by identity, as %s", where, field.String, pathOrRoot(at), exampleName(identity))
			}
			if !name.Binds(identity) {
				return fmt.Errorf("write at %q: %s does not name an element of %s, whose identity is %s",
					where, name, pathOrRoot(at), strings.Join(identity, ","))
			}
			if err := stampElement(n.Values[i], name); err != nil {
				return fmt.Errorf("write at %q: %w", where, err)
			}
			if err := lowerKeyedRec(schema, n.Values[i], at, true, where); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

// keyCarrier is the node an element's identity is read from: the element, or for a
// !replace the value it installs.
func keyCarrier(elem *ir.Node) *ir.Node {
	e := ir.Uncomment(elem)
	if e != nil && opOf(e) == "replace" {
		if to := ir.Get(e, "to"); to != nil {
			return to
		}
		return ir.Get(e, "from")
	}
	return e
}

// stampElement puts the name's identity into an element that lacks it and refuses one
// that disagrees. A !delete carries nothing that has to agree; a !replace is held to it on
// both sides.
func stampElement(elem *ir.Node, name ident.Name) error {
	e := ir.Uncomment(elem)
	if e == nil {
		return nil
	}
	switch opOf(e) {
	case "delete":
		return nil
	case "replace":
		for _, side := range []string{"from", "to"} {
			if v := ir.Get(e, side); v != nil && ir.Uncomment(v) != nil && ir.Uncomment(v).Type == ir.ObjectType {
				if err := name.Stamp(v); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if e.Type != ir.ObjectType {
		return fmt.Errorf("the element named %s must be an object, and this write makes it %s", name, e.Type)
	}
	return name.Stamp(e)
}

func sortByName(fields, values []*ir.Node) {
	idx := make([]int, len(fields))
	for i := range idx {
		idx[i] = i
	}
	slices.SortFunc(idx, func(a, b int) int { return strings.Compare(fields[a].String, fields[b].String) })
	f2 := make([]*ir.Node, len(fields))
	v2 := make([]*ir.Node, len(values))
	for i, j := range idx {
		f2[i], v2[i] = fields[j], values[j]
	}
	copy(fields, f2)
	copy(values, v2)
}

// opOf is the merge operation a node carries, or "" for none. Labels that are not
// operations -- presentation, logd's own markers -- are not operations here either.
func opOf(n *ir.Node) string {
	if n == nil || n.Tag == "" {
		return ""
	}
	_, op, _, _, err := mergeop.SplitChild(n)
	if err != nil {
		return ""
	}
	return op
}

func pathOrRoot(kpath string) string {
	if kpath == "" {
		return "the document root"
	}
	return kpath
}
