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
		resMap[key] = keyVal
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
