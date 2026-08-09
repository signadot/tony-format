package libdiff

import (
	"bytes"
	"fmt"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

func DiffArrayByKey(from, to *ir.Node, key string, df DiffFunc) (*ir.Node, error) {
	// keyNodes keeps each key as the node it was written as, so the result can carry it
	// across rather than re-parsing its rendering (see yKeyNodeOf). `to` overwrites
	// `from`, so a key that survives is described the way the target holds it, and a
	// key only `from` has -- a removal -- still has its node.
	keyNodes := make(map[string]*ir.Node, len(from.Values)+len(to.Values))

	fromMap := make(map[string]*ir.Node, len(from.Values))
	fromTagMap := make(map[string]string)
	for _, val := range from.Values {
		keyNode, valKey, vkTag, err := yKeyNodeOf(val, key)
		if err != nil {
			return nil, err
		}
		fromMap[valKey] = val
		keyNodes[valKey] = keyNode
		if vkTag != "" {
			fromTagMap[valKey] = vkTag
		}
	}
	toMap := make(map[string]*ir.Node, len(to.Values))
	toTagMap := make(map[string]string)
	for _, val := range to.Values {
		keyNode, valKey, vkTag, err := yKeyNodeOf(val, key)
		if err != nil {
			return nil, err
		}
		toMap[valKey] = val
		keyNodes[valKey] = keyNode
		if vkTag != "" {
			toTagMap[valKey] = vkTag
		}
	}
	fromObj := ir.FromMap(fromMap).WithTag(from.Tag)
	toObj := ir.FromMap(toMap).WithTag(to.Tag)
	// df reports "no difference" as a nil node rather than as an object holding
	// nothing, and two keyed lists holding the same elements under the same keys is the
	// ordinary case, not a corner one: most changes leave most lists alone. Indexing
	// that nil is a panic, and the branch reaching it only runs when BOTH sides carry
	// !key(f) with the same field -- which no materialized state does today (the op is
	// resolved at apply time), so nothing had exercised it.
	//
	// An empty result is left to the tail below, which already knows what it means and
	// still reports a change to the list's OWN tag.
	objDiff := df(fromObj, toObj)
	var resItems []*ir.Node
	if objDiff != nil {
		resItems = make([]*ir.Node, len(objDiff.Values))
	}
	for i := range resItems {
		v := objDiff.Values[i]
		var resMap map[string]*ir.Node
		switch v.Type {
		case ir.ObjectType:
			resMap = ir.ToMap(v)
		case ir.NullType:
			resMap = map[string]*ir.Node{}
		default:
			return nil, fmt.Errorf("wrong type for value: %s", v.Type)
		}
		keyValStr := objDiff.Fields[i].String
		keyNode := keyNodes[keyValStr]
		if keyNode == nil {
			// Every field of objDiff came from fromMap or toMap, both of which record
			// their key node here, so this cannot happen without one of them lying.
			return nil, fmt.Errorf("no key node recorded for %q", keyValStr)
		}
		keyVal := keyNode.Clone()
		fkvTag := fromTagMap[keyValStr]
		tkvTag := toTagMap[keyValStr]
		if fkvTag != tkvTag {
			keyVal.Tag = MakeTagDiff(fkvTag, tkvTag)
		} else {
			keyVal.Tag = fkvTag
		}
		if err := placeKey(resMap, key, keyVal); err != nil {
			return nil, err
		}
		item := ir.FromMap(resMap)
		item.Tag = v.Tag
		resItems[i] = item
	}
	if len(resItems) == 0 {
		if from.Tag != to.Tag {
			return ir.Null().WithTag(MakeTagDiff(from.Tag, to.Tag)), nil
		}
		return nil, nil
	}
	res := ir.FromSlice(resItems)
	if from.Tag != to.Tag {
		return res.WithTag(MakeTagDiff(from.Tag, to.Tag)), nil
	}
	res.Tag = from.Tag
	return res, nil
}

// placeKey puts a rebuilt element's key back where the key field says it lives.
//
// The key field is a PATH, not just a field name: !key(meta.name) is legal, and both
// readers of a key -- ir.ElemKey and YKeyOf's GetPath -- resolve it as one. Writing it
// as a single flat field named "meta.name" therefore produced an element whose own key
// no one could find, and a patch carrying it failed with "no meta.name to merge by"
// while listing "meta.name" among the fields it had.
//
// The element's diff may already hold part of that path, when a sibling under the same
// parent also changed, so each level merges into what is there rather than replacing it.
func placeKey(resMap map[string]*ir.Node, key string, keyVal *ir.Node) error {
	p, err := ir.ParsePath("$." + key)
	if err != nil {
		return err
	}
	if p == nil || p.Field == nil {
		return fmt.Errorf("key %q does not name a field", key)
	}
	if p.Next == nil {
		resMap[*p.Field] = keyVal
		return nil
	}
	child, err := placeKeyIn(resMap[*p.Field], p.Next, keyVal, key)
	if err != nil {
		return err
	}
	resMap[*p.Field] = child
	return nil
}

// placeKeyIn returns node with keyVal placed at p inside it, building the objects the
// diff does not already have. A non-object already sitting on the path is reported
// rather than overwritten: the key would be unreachable either way, and saying so beats
// discarding whatever the diff meant to say there.
func placeKeyIn(node *ir.Node, p *ir.Path, keyVal *ir.Node, key string) (*ir.Node, error) {
	if p.Field == nil {
		return nil, fmt.Errorf("key %q: only field segments can be rebuilt", key)
	}
	var m map[string]*ir.Node
	switch {
	case node == nil:
		m = map[string]*ir.Node{}
	case node.Type == ir.ObjectType:
		m = ir.ToMap(node)
	default:
		return nil, fmt.Errorf("key %q: cannot place it under a %s", key, node.Type)
	}
	if p.Next == nil {
		m[*p.Field] = keyVal
	} else {
		child, err := placeKeyIn(m[*p.Field], p.Next, keyVal, key)
		if err != nil {
			return nil, err
		}
		m[*p.Field] = child
	}
	res := ir.FromMap(m)
	if node != nil {
		res.Tag = node.Tag
	}
	return res, nil
}

func YKeyOf(y *ir.Node, key string) (string, string, error) {
	_, s, tag, err := yKeyNodeOf(y, key)
	return s, tag, err
}

// yKeyNodeOf is YKeyOf plus the key NODE. The rendered form identifies an element, but
// it is lossy as a value: rebuilding a key by re-parsing its rendering turns a quoted
// key into a bare one, so the rebuilt element no longer compares equal to the original
// even though the two encode identically. Keeping the node lets the diff carry the key
// across unchanged.
func yKeyNodeOf(y *ir.Node, key string) (*ir.Node, string, string, error) {
	v, err := y.GetPath("$." + key)
	if err != nil {
		return nil, "", "", err
	}
	orgTag := v.Tag
	defer func() { v.Tag = orgTag }()
	v.Tag = ""
	buf := bytes.NewBuffer(nil)
	if err := encode.Encode(v, buf); err != nil {
		return nil, "", "", err
	}
	return v, buf.String(), orgTag, nil
}
