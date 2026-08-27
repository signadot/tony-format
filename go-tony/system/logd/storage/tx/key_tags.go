package tx

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	kpathpkg "github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// InjectKeyTags puts !key(f) on every array a patch writes whose path the schema declares
// keyed, so the MERGE identifies elements the same way the INDEX does.
//
// Without it the two disagree, silently and in the direction that loses data: the schema
// is handed to IndexPatch, so a write of {items: [{sku: "G"}]} to a declared-keyed array
// is recorded at items("G"), while the merge -- which reads only the tag on the patch --
// treats the array positionally and REPLACES whatever sat at index 0. Declaring a key had
// no effect on what a write meant.
//
// Injecting rather than rejecting is a choice about whose job it is: the schema says these
// elements have identities, so a client that names one should not also have to repeat how
// identity works on every write. A patch that carries its own !key for the same array is
// left alone if it agrees, and refused if it does not — two identities for one array is
// the ambiguity Schema.Validate already refuses to adopt, and it is no better arriving one
// write at a time.
//
// Paths follow InjectAutoIDs: each patcher's data starts at its own API.Path, object
// fields extend it, and the elements of a keyed array inherit the array's path — which is
// what api.AutoIDField.Path ("orders.items") and indexPatchRec both mean by it.
func InjectKeyTags(schema *api.Schema, data []*PatcherData) error {
	if schema == nil {
		return nil
	}
	for _, pd := range data {
		if pd.API == nil || pd.API.Data == nil {
			continue
		}
		if err := injectKeyTagsRec(schema, pd.API.Data, pd.API.Path); err != nil {
			return err
		}
	}
	return nil
}

func injectKeyTagsRec(schema *api.Schema, node *ir.Node, kpath string) error {
	if node == nil {
		return nil
	}

	// A comment wraps the value it precedes, and the !key tag belongs to the array
	// inside the wrapper, not to what was said about it. Unwrapping here tags that
	// array in place, leaving the wrapper above it; left wrapped, a commented array
	// was never keyed and its elements merged positionally (3cdjz00jh12krns4g1n0).
	node = ir.Uncomment(node)

	// A !raw subtree is a document the store CARRIES rather than one it owns: the
	// escape says to treat it as data, interpreting no operation at any depth, and
	// it lands with its own tags intact. Reshaping it is the store editing someone
	// else's document -- a rule, a charter, a patch, which is what !raw is for.
	//
	// Both walks stopped here for the same reason and neither did: an operator tag
	// went straight in (5hmq80f3h12krh1mbsn0).
	if ir.TagHas(node.Tag, libdiff.RawTag) {
		return nil
	}

	switch node.Type {
	case ir.ObjectType:
		for i, field := range node.Fields {
			if i >= len(node.Values) {
				continue
			}
			childPath := kpathpkg.ChildField(kpath, field.String)
			if err := injectKeyTagsRec(schema, node.Values[i], childPath); err != nil {
				return err
			}
		}

	case ir.ArrayType:
		declared := schema.LookupKeyField(kpath)
		if declared != "" {
			have, keyed := node.KeyField()
			switch {
			case !keyed:
				node.Tag = ir.TagCompose(ir.KeyTag, []string{declared}, node.Tag)
			case have != declared:
				return fmt.Errorf("array at %q is declared keyed by %q but the patch keys it by %q; "+
					"one array has one identity", pathOrRoot(kpath), declared, keyName(have))
			}
			// Elements of a keyed array inherit its path, so a nested declaration like
			// "orders.items" is reachable from "orders".
			for _, elem := range node.Values {
				if err := injectKeyTagsRec(schema, elem, kpath); err != nil {
					return err
				}
			}
			return nil
		}
		for i, elem := range node.Values {
			if err := injectKeyTagsRec(schema, elem, fmt.Sprintf("%s[%d]", kpath, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func pathOrRoot(kpath string) string {
	if kpath == "" {
		return "the document root"
	}
	return kpath
}

// keyName renders a patch's own key field for an error, including the bare !key case,
// which names no field at all.
func keyName(field string) string {
	if field == "" {
		return "the element itself (a bare !key)"
	}
	return field
}
