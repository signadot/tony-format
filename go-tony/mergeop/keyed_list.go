package mergeop

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var keyedListSym = &keyedListSymbol{name: keyedListName}

func KeyedList() Symbol {
	return keyedListSym
}

const (
	keyedListName name = "key"
)

type keyedListSymbol struct {
	name
}

func (s keyedListSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s op expects 1 arg (yamlpath key), got %v", s, args)
	}
	key := ""
	if len(args) > 0 {
		key = args[0]
	}
	return &keyedListOp{key: key, op: op{name: s.name, child: child}}, nil
}

type keyedListOp struct {
	op
	key string
}

func (kl keyedListOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("patch op key on %s\n", doc.Path())
	}
	klMap := make(map[string]*ir.Node, len(kl.child.Values))
	for i, klItem := range kl.child.Values {
		key, ok, err := yKeyOf(klItem, kl.key)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, kl.errNoKey(i, klItem)
		}
		klMap[key] = klItem
	}
	dst := make([]*ir.Node, len(doc.Values))
	for i := range doc.Values {
		dst[i] = doc.Values[i].Clone()
	}
	for i, docItem := range dst {
		key, ok, err := yKeyOf(docItem, kl.key)
		if err != nil {
			return nil, err
		}
		if !ok {
			// A document element without the key is not an error the way a
			// patch element without it is: no patch element can address it, so
			// it is simply not one of the ones being merged. It stays in the
			// list untouched rather than failing a patch that never named it.
			continue
		}
		patchObj, ok := klMap[key]
		if !ok {
			//fmt.Printf("no patch for key %q\n", key)
			continue
		}
		v, err := pf(docItem, patchObj, ctx)
		if err != nil {
			return nil, err
		}
		//fmt.Printf("patched key %q\npatch\n%s\nres\n%s", key, patchObj.MustString(), v.MustString())
		// v is nil when the patch for this key removed the item, as !delete
		// does; the item leaves the list rather than becoming a hole in it.
		dst[i] = v
		delete(klMap, key)
	}
	res := make([]*ir.Node, 0, len(dst)+len(klMap))
	for _, v := range dst {
		if v == nil {
			continue
		}
		res = append(res, v)
	}
	keys := slices.Sorted(maps.Keys(klMap))
	for _, key := range keys {
		patchChild := klMap[key]
		res = append(res, patchChild)
	}
	// patching the items of a list does not change what the list is, so it
	// keeps its own tag -- !key(...) above all, without which the result is no
	// longer a keyed list.
	return ir.FromSlice(res).WithTag(doc.Tag), nil
}

func (kl keyedListOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("key(%s) op match on %s\n", kl.key, doc.Path())
	}
	if doc.Type != ir.ArrayType {
		return false, nil
	}
	klMap := make(map[string]*ir.Node, len(kl.child.Values))
	for i, klItem := range kl.child.Values {
		// TODO match key tag
		key, ok, err := yKeyOf(klItem, kl.key)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, kl.errNoKey(i, klItem)
		}
		klMap[key] = klItem
		if debug.Op() {
			debug.Logf("\tkey %s=%q (tag of item is %q)\n", kl.key, key, klItem.Tag)
		}
	}
	matched := 0
	for _, docItem := range doc.Values {
		// TODO match w/ tagged key
		key, ok, err := yKeyOf(docItem, kl.key)
		if err != nil {
			return false, err
		}
		if !ok {
			// No key, so nothing in the patch names it: not a match, not an
			// error. Same reasoning as the document side of Patch.
			continue
		}
		matchObj, ok := klMap[key]
		if !ok {
			continue
		}
		match, err := f(docItem, matchObj, ctx)
		if err != nil {
			return false, err
		}
		if !match {
			continue
		}
		matched++
		if matched == len(kl.child.Values) {
			break
		}
	}
	return matched == len(kl.child.Values), nil
}

// yKeyOf renders the merge key of y as a string. The bool reports whether y
// carries the key at all: GetPath answers a missing field with (nil, nil), so
// absence is not an error here and callers decide what it means -- see the two
// call sites in Patch, which differ.
func yKeyOf(y *ir.Node, key string) (string, bool, error) {
	p := key
	if p == "" {
		p = "$"
	} else if p[0] != '[' {
		p = "$." + p
	} else {
		p = "$" + p
	}
	v, err := y.GetPath(p)
	if err != nil {
		return "", false, err
	}
	if v == nil {
		return "", false, nil
	}
	buf := bytes.NewBuffer(nil)
	orgTag := v.Tag
	defer func() { v.Tag = orgTag }()
	v.Tag = ""
	if err := encode.Encode(v, buf); err != nil {
		return "", false, err
	}
	return buf.String(), true, nil
}

// errNoKey reports a patch element that cannot be merged because it does not
// carry the key the list is merged by. It names the fields the element does
// have, since the usual cause is a typo or -- as in the report this came from
// -- an attempt to write "the only element" by leaving the key out.
func (kl keyedListOp) errNoKey(i int, item *ir.Node) error {
	have := ""
	if item.Type == ir.ObjectType && len(item.Fields) > 0 {
		names := make([]string, 0, len(item.Fields))
		for _, f := range item.Fields {
			names = append(names, f.String)
		}
		have = fmt.Sprintf(" (it has %s)", strings.Join(names, ", "))
	}
	return fmt.Errorf(
		"!%s(%s): patch element %d has no %q to merge by%s; a keyed-list element without its key matches nothing and cannot be placed",
		kl.name, kl.key, i, kl.key, have)
}
