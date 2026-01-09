package mergeop

import (
	"bytes"
	"fmt"
	"maps"
	"slices"

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
	for _, klItem := range kl.child.Values {
		key, _, err := yKeyOf(klItem, kl.key)
		if err != nil {
			return nil, err
		}
		klMap[key] = klItem
	}
	dst := make([]*ir.Node, len(doc.Values))
	for i := range doc.Values {
		dst[i] = doc.Values[i].Clone()
	}
	for i, docItem := range dst {
		key, _, err := yKeyOf(docItem, kl.key)
		if err != nil {
			return nil, err
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
		dst[i] = v
		delete(klMap, key)
	}
	keys := slices.Sorted(maps.Keys(klMap))
	for _, key := range keys {
		patchChild := klMap[key]
		dst = append(dst, patchChild)
	}
	return ir.FromSlice(dst), nil
}

func (kl keyedListOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("key(%s) op match on %s\n", kl.key, doc.Path())
	}
	if doc.Type != ir.ArrayType {
		return false, nil
	}
	klMap := make(map[string]*ir.Node, len(kl.child.Values))
	for _, klItem := range kl.child.Values {
		// TODO match key tag
		key, _, err := yKeyOf(klItem, kl.key)
		if err != nil {
			return false, err
		}
		klMap[key] = klItem
		if debug.Op() {
			debug.Logf("\tkey %s=%q (tag of item is %q)\n", kl.key, key, klItem.Tag)
		}
	}
	matched := 0
	for _, docItem := range doc.Values {
		// TODO match w/ tagged key
		key, _, err := yKeyOf(docItem, kl.key)
		if err != nil {
			return false, err
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

func yKeyOf(y *ir.Node, key string) (string, string, error) {
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
		return "", "", err
	}
	buf := bytes.NewBuffer(nil)
	orgTag := v.Tag
	defer func() { v.Tag = orgTag }()
	v.Tag = ""
	if err := encode.Encode(v, buf); err != nil {
		return "", "", err
	}
	return buf.String(), orgTag, nil
}
