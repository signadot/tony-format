package libdiff

import (
	"bytes"
	"fmt"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func DiffArrayByKey(from, to *ir.Node, key string, df DiffFunc) (*ir.Node, error) {
	fromMap := make(map[string]*ir.Node, len(from.Values))
	fromTagMap := make(map[string]string)
	for _, val := range from.Values {
		valKey, vkTag, err := YKeyOf(val, key)
		if err != nil {
			return nil, err
		}
		fromMap[valKey] = val
		if vkTag != "" {
			fromTagMap[valKey] = vkTag
		}
	}
	toMap := make(map[string]*ir.Node, len(to.Values))
	toTagMap := make(map[string]string)
	for _, val := range to.Values {
		valKey, vkTag, err := YKeyOf(val, key)
		if err != nil {
			return nil, err
		}
		toMap[valKey] = val
		if vkTag != "" {
			toTagMap[valKey] = vkTag
		}
	}
	fromObj := ir.FromMap(fromMap).WithTag(from.Tag)
	toObj := ir.FromMap(toMap).WithTag(to.Tag)
	objDiff := df(fromObj, toObj)
	resItems := make([]*ir.Node, len(objDiff.Values))
	for i, v := range objDiff.Values {
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
		keyVal, err := parse.Parse([]byte(keyValStr))
		if err != nil {
			return nil, err
		}
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
	v, err := y.GetPath("$." + key)
	if err != nil {
		return "", "", err
	}
	orgTag := v.Tag
	defer func() { v.Tag = orgTag }()
	v.Tag = ""
	buf := bytes.NewBuffer(nil)
	if err := encode.Encode(v, buf); err != nil {
		return "", "", err
	}
	return buf.String(), orgTag, nil
}
