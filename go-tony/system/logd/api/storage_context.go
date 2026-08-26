package api

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/schema"
)

// The vocabulary a STORED logd delta may use, and the one place both of logd's storage
// restrictions live.
//
// A patch may be written with arbitrary expressivity. What logd STORES is lowered to this
// vocabulary, in which the data reflects the result: an operation leaves no residual
// meaning once applied. Two reasons, and they are different reasons:
//
//   - A store's user believes they are working with data. An op that re-evaluates later
//     breaks that belief, and a store cannot know what mergeops will exist next year.
//   - A scope's base MOVES underneath it (baseline advances), so a relative op stored in a
//     scope layer recomputes against something other than what it was written against.
//     Baseline gets away with arbitrary ops only because its replay is deterministic.
//
// This is deliberately NOT an extension of tony-format/context/patch. It is a narrower
// vocabulary declared in full, so adding a mergeop to tony does not silently make it
// storable here.
//
// The second restriction is the index's, not the format's: see ValidateForStorage.

// StorageContextURI names the context in the schema registry.
const StorageContextURI = "logd/context/storage"

// storableTags is the vocabulary. Absolute and unconditional: applying one leaves data
// that reflects the result, and re-applying it to a base that has moved does not consult
// what used to be there.
//
// Deliberately absent, with reasons, because the omissions are the point:
//
//	replace    CHECKED -- verifies the document still equals from:, so against a moved
//	           baseline it errors outright rather than applying. Lowering rewrites it.
//	retag      CHECKED for the same reason, and less obviously: retagOp refuses unless the
//	           document's tag already equals from. It reads like a statement of the
//	           resulting tag and behaves like an assertion about the previous one. addtag
//	           and rmtag are the unconditional halves and are storable.
//	strdiff    relative to the string that was there
//	arraydiff  relative to the array that was there, and positional
//	rename     relative to the keys that were there; lowers to delete + insert
//	jsonpatch  a sequence relative to the document
//	if, let    conditional on the document
//	quote, unquote, nullify, dive, embed, pass
//	           transforms of whatever is found, not statements of what is
//	pipe       calls out to the system, so storing one means re-running it per replay
//
// comment is storable for the same reason addtag is: it states what the comment
// IS. Without it a comment change could only be written as a replacement of the
// value it describes -- the whole subtree, twice -- which is neither storable nor
// proportionate to an edit of one line of text.
var storableTags = map[string]string{
	"insert":  "adds a value; the value is what results",
	"delete":  "removes a value; absence is what results",
	"key":     "identifies array elements so a merge is by identity rather than position",
	"raw":     "escapes data that would otherwise read as an operation",
	"addtag":  "adds a tag; the tag is what results",
	"rmtag":   "removes a tag; its absence is what results",
	"comment": "states the comments at a node; the lines are what results, and none removes one",
}

// StorageContext is logd's storage vocabulary as a schema context.
func StorageContext() *schema.Context {
	tags := make(map[string]*schema.TagDefinition, len(storableTags))
	for name, why := range storableTags {
		tags[name] = &schema.TagDefinition{
			Name:        name,
			Contexts:    []string{StorageContextURI},
			Description: why,
		}
	}
	tags["key"].SchemaRef = StorageContextURI + "/key"
	tags["key"].Description += "; logd additionally requires each element's key to resolve" +
		" to a scalar and to be unique once rendered -- see ValidateForStorage"
	return &schema.Context{
		URI:       StorageContextURI,
		ShortName: "logd-storage",
		Tags:      tags,
	}
}

// IsStorableTag reports whether an op may appear in a stored delta. The name is the
// mergeop name without its '!'.
func IsStorableTag(name string) bool {
	_, ok := storableTags[name]
	return ok
}

// ValidateForStorage checks a node against both restrictions.
//
// The first is the vocabulary above. The second is the INDEX's, and it is narrower than
// what a merge accepts: indexPatchRec turns each keyed element into a path segment via
// ir.ElemKey, which admits only a scalar, while mergeop's yKeyOf encodes any node at all.
// An object-valued key or a bare !key is therefore ordinary in a merge and unrepresentable
// in the index, where it collapses every element onto items("") -- silently, because
// indexPatchRec discards ElemKey's second return. Rendering also loses type, so 1 and "1"
// are two elements sharing one path.
//
// That belongs here rather than in tony: encoding any node as a merge key is meaningful
// for a merge, and requiring a renderable, injective path segment is a consequence of
// having an index.
func ValidateForStorage(n *ir.Node) error {
	return validateForStorage(n, "")
}

func validateForStorage(n *ir.Node, path string) error {
	if n == nil {
		return nil
	}
	if _, op, _, _, err := mergeop.SplitChild(n); err == nil && op != "" && !IsStorableTag(op) {
		return fmt.Errorf("%s: %w", at(path), fmt.Errorf("operation %q may not be stored: %s",
			"!"+op, whyNotStorable(op)))
	}
	// !raw says nothing beneath is interpreted, so nothing beneath is an
	// operation to hold to this vocabulary -- it is data that happens to be
	// shaped like one. Walking into it refuses the one escape that lets a
	// document holding operators be stored at all, which is what a charter, a
	// stored rule and a stored patch are; and refusing it here refuses a write
	// the writer escaped correctly (issue 6225etzfh12kr955fxn0).
	//
	// The node's own tag chain is still checked above: !strdiff.raw is refused
	// for the strdiff, while !insert.raw is allowed and stops here.
	if ir.TagHas(n.Tag, "!raw") {
		return nil
	}
	// A head comment wraps the value it precedes, and this walk asks what KIND of
	// node it is standing on. A comment is not a kind of container, so the walk
	// stopped at the wrapper and nothing beneath it was checked: an unstorable
	// operation written under a comment passed validation and was stored. Now that
	// a store keeps comments, that is reachable from any client (3cdjz00jh12krns4g1n0).
	n = ir.Uncomment(n)
	if n.Type == ir.ArrayType {
		if err := validateKeyedArray(n, path); err != nil {
			return err
		}
	}
	switch n.Type {
	case ir.ObjectType:
		for i, f := range n.Fields {
			p := kpath.ChildField(path, f.String)
			if i < len(n.Values) {
				if err := validateForStorage(n.Values[i], p); err != nil {
					return err
				}
			}
		}
	case ir.ArrayType:
		for i, v := range n.Values {
			if err := validateForStorage(v, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateKeyedArray applies the index's requirements to a !key list.
func validateKeyedArray(n *ir.Node, path string) error {
	field, keyed := n.KeyField()
	if !keyed {
		return nil
	}
	if field == "" {
		return fmt.Errorf("%s: a bare !key cannot be stored: the index turns each element "+
			"into a path segment from a scalar field, and keying by the element itself has "+
			"no such field, so every element would collapse onto one path", at(path))
	}
	seen := map[string]int{}
	for i, elem := range n.Values {
		k, ok := ir.ElemKey(elem, field)
		if !ok {
			return fmt.Errorf("%s: element %d has no scalar at %q to key by; the index can "+
				"render a string, number or bool as a path segment and nothing else", at(path), i, field)
		}
		if prev, dup := seen[k]; dup {
			return fmt.Errorf("%s: elements %d and %d both render their key %q as %q, so the "+
				"index would hold one path for two elements", at(path), prev, i, field, k)
		}
		seen[k] = i
	}
	return nil
}

func at(path string) string {
	if path == "" {
		return "at the document root"
	}
	return "at " + path
}

func whyNotStorable(op string) string {
	switch op {
	case "replace", "retag":
		return "it is checked, so against a base that has moved it errors rather than applying"
	case "strdiff", "arraydiff", "rename", "jsonpatch":
		return "its result depends on what was there, so it re-evaluates against a base that has moved"
	case "if", "let":
		return "it is conditional on the document it meets"
	case "get-path", "get-paths":
		return "it answers with a value read from elsewhere in the document, so against a " +
			"base that has moved it answers with a different one"
	case "pipe":
		return "it calls out to the system, so storing it means re-running it on every replay"
	default:
		return "it transforms whatever it finds rather than stating what results"
	}
}
