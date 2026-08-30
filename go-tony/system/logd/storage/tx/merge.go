package tx

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// MergePatches merges multiple api patches into a single ir node patch.
//
// Example:
//   - Patches at "a.b" and "a.c" → {a: {b: <patch1>, c: <patch2>}}
//   - Patches at "a.b" and "x.y" → {a: {b: <patch1>}, x: {y: <patch2>}}
//   - Patch at ""  → <patch> (root-level patch, not wrapped)
//
// Returns an error if any kpath of an api patch is a descendent of another
// or any patcherdata path is invalid or if the paths are incompatible
// such as a.b and a[1].
func MergePatches(patches []*PatcherData) (*ir.Node, error) {
	if len(patches) == 0 {
		return nil, nil
	}

	splitPaths := make([][]string, len(patches))
	for i, pd := range patches {
		_, err := kpath.Parse(pd.API.Path)
		if err != nil {
			return nil, err
		}
		splitPaths[i] = kpath.SplitAll(pd.API.Path)
	}
	for i := range splitPaths {
		for j := range i {
			spi, spj := splitPaths[i], splitPaths[j]
			if isPrefix(spi, spj) {
				return nil, fmt.Errorf("patch at %s conflicts with %s", strings.Join(spj, ""), strings.Join(spi, ""))
			}
		}
	}
	root := &kTree{children: make(map[string]*kTree)}
	for _, pd := range patches {
		kp, node := pd.API.Path, pd.API.Data
		if err := root.add(kp, node); err != nil {
			return nil, err
		}
	}
	return root.node()
}

func isPrefix(a, b []string) bool {
	if len(b) < len(a) {
		a, b = b, a
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type kTree struct {
	*kpath.KPath
	children map[string]*kTree
	ir       *ir.Node
}

func newKTree(kp string, node *ir.Node) (*kTree, error) {
	res := &kTree{
		children: map[string]*kTree{},
	}
	if kp == "" {
		res.ir = node
		return res, nil
	}
	kPath, err := kpath.Parse(kp)
	if err != nil {
		return nil, err
	}
	kt := &kTree{
		KPath:    kPath,
		children: map[string]*kTree{},
	}
	res.children[kPath.SegmentString()] = kt
	cur := kt
	for cur.Next != nil {
		nxt := cur.Next
		tNext := &kTree{
			KPath:    nxt,
			children: map[string]*kTree{},
		}
		cur.children[nxt.SegmentString()] = tNext
		cur = tNext
	}
	cur.ir = node
	return res, nil
}

// RootPatchAt wraps node as a patch rooted at the document root under kp, using
// the same canonical rooting as a client patch (object fields, sparse {n}, and
// array [i] via !arraydiff). kp == "" returns node unchanged. This is the inverse
// of navigating to kp: a delta computed at kp's subtree is re-rooted for the
// root-rooted watch delta contract.
func RootPatchAt(kp string, node *ir.Node) (*ir.Node, error) {
	kt, err := newKTree(kp, node)
	if err != nil {
		return nil, err
	}
	return kt.node()
}

// RootKeyedListAt roots elems at an ARRAY path as a keyed list, which is how a
// patch names elements by identity rather than by position.
//
// It exists because RootPatchAt cannot take the element path itself: `items("A")`
// carries the key VALUE where building the patch needs the key FIELD. Both callers
// worked around that the same way -- root at the array, carry a keyed list -- and
// the plan asked for the helper rather than let it be found a third time
// (docs/archive/scope_overlay_plan.md, P1).
//
// The tag is what makes the merge identify elements: without it the same list merges
// POSITIONALLY, replacing whatever sits at index 0. That is the failure tx.InjectKeyTags
// exists to prevent on a client write, and the same one here.
func RootKeyedListAt(arrayPath, keyField string, elems ...*ir.Node) (*ir.Node, error) {
	if keyField == "" {
		return nil, errors.New("keyed list needs a key field")
	}
	list := ir.FromSlice(elems)
	list.Tag = ir.TagCompose(ir.KeyTag, []string{keyField}, "")
	return RootPatchAt(arrayPath, list)
}

func (kt *kTree) add(kp string, node *ir.Node) error {
	ot, err := newKTree(kp, node)
	if err != nil {
		return err
	}
	return kt.merge(ot)
}

func (kt *kTree) merge(ot *kTree) error {
	if err := kt.compat(ot); err != nil {
		return err
	}
	if ot.ir != nil {
		if kt.ir != nil {
			return errors.New("overwrite")
		}
		kt.ir = ot.ir
		return nil
	}
	for seg, oc := range ot.children {
		kc := kt.children[seg]
		if kc == nil {
			kt.children[seg] = oc
			continue
		}
		if err := kc.merge(oc); err != nil {
			return err
		}
	}
	return nil
}

func (kt *kTree) node() (*ir.Node, error) {
	if kt.ir != nil {
		return kt.ir, nil
	}
	switch kt.childKind() {
	case unknownKind:
		return nil, errors.New("ir node unspecified")
	case keyedArrayKind:
		// A key segment names an element by its key VALUE, and building the patch
		// needs the key FIELD, which the path does not carry. This used to fall
		// through to "ir node unspecified", which says nothing about the cause and
		// left both callers to rediscover the same workaround (5hmq80f3h12krh1mbsn0).
		return nil, fmt.Errorf("cannot root a patch at a keyed element path: a key " +
			"segment names an element by value and building the patch needs the key " +
			"field, which the path does not carry -- use RootKeyedListAt with the field")
	case objectKind:
		m := map[string]*ir.Node{}
		for f, child := range kt.children {
			cNode, err := child.node()
			if err != nil {
				return nil, err
			}
			// Use the canonical (unquoted) field name, not the raw segment string.
			// SegmentString re-quotes a key that needs quoting (a digit-first key
			// becomes "9digit"); storing that quoted form as the object key makes it
			// fail to match an unquoted pattern field later — the CAS precondition
			// over a digit-first subtree reads null and the commit is revoked.
			// KPath.Field is already the unquoted name.
			if child.KPath != nil && child.KPath.Field != nil {
				m[*child.KPath.Field] = cNode
				continue
			}
			m[strings.TrimPrefix(f, ".")] = cNode
		}
		return ir.FromMap(m), nil
	case sparseArrayKind:
		m := map[uint32]*ir.Node{}
		for i, child := range kt.children {
			// chop leading and trailing {}
			// these were generated by ir pkg and checked
			// for compat so this should be safe.
			i := i[1:]
			i = i[:len(i)-1]
			ii, err := strconv.ParseUint(i, 10, 64)
			if err != nil {
				return nil, err
			}
			cNode, err := child.node()
			if err != nil {
				return nil, err
			}
			m[uint32(ii)] = cNode
		}
		return ir.FromIntKeysMap(m), nil
	case arrayKind:
		// in this case we need to construct a patch node
		// for 2 arrays with individual sub-patches at disjoint
		// indices, we create an !arraydiff int keys map like
		// libdiff.DiffArrayByIndex
		// Note: paths are non-wildcard by construction, so [*] cannot occur
		m := map[uint32]*ir.Node{}
		for i, child := range kt.children {
			// Strip leading [ and trailing ]
			idxStr := i[1 : len(i)-1]
			ii, err := strconv.ParseUint(idxStr, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid array index %q: %w", i, err)
			}
			cNode, err := child.node()
			if err != nil {
				return nil, err
			}
			m[uint32(ii)] = cNode
		}
		return ir.FromIntKeysMap(m).WithTag("!arraydiff"), nil
	default:
		panic("child kind")
	}
}

func (kt *kTree) compat(ot *kTree) error {
	if (kt.KPath == nil) != (ot.KPath == nil) {
		return errors.New("misaligned root")
	}
	kk := kt.childKind()
	if kk == unknownKind {
		return nil
	}
	ok := ot.childKind()
	if ok == unknownKind {
		return nil
	}
	if kk != ok {
		return errors.New("mixed accessors")
	}
	return nil
}

func (kt *kTree) childKind() childKind {
	kind := unknownKind
	for _, ct := range kt.children {
		if ct.Field != nil || ct.FieldAll {
			return objectKind
		}
		if ct.Key != nil {
			return keyedArrayKind
		}
		if ct.Index != nil || ct.IndexAll {
			return arrayKind
		}
		if ct.SparseIndex != nil || ct.SparseIndexAll {
			return sparseArrayKind
		}
	}
	return kind
}

type childKind int

const (
	unknownKind childKind = iota
	objectKind
	arrayKind
	sparseArrayKind
	keyedArrayKind
)
