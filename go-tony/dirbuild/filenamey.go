package dirbuild

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
)

type fileNamer struct {
	APIVersion string `tony:"field=apiVersion"`
	Kind       string `tony:"field=kind"`
	Type       string `tony:"field=type"`
	Metadata   struct {
		Namespace string `tony:"field=namespace"`
		Name      string `tony:"field=name"`
	} `tony:"field=metadata"`
	Name string `tony:"field=name"`
}

func (fn *fileNamer) FileName() string {
	var (
		name      string
		namespace string
	)
	if fn.Name != "" {
		if fn.Type != "" {
			return fn.Type + "-" + fn.Name
		}
		return fn.Name
	}

	namespace = fn.Metadata.Namespace
	if namespace == "" {
		namespace = "default"
	}
	name = fn.Metadata.Name
	if name == "" {
		name = "obj"
	}
	return name + "-" + strings.ToLower(fn.Kind) + "-" + namespace
}

func fileName(node *ir.Node) string {
	switch node.Type {
	case ir.ObjectType:
		name := &fileNamer{}
		if err := gomap.FromTonyIR(node, name); err != nil {
			return "obj"
		}
		return name.FileName()
	case ir.ArrayType:
		if len(node.Values) == 0 {
			return "arr"
		}
		return "arr-" + fileName(node.Values[0])
	case ir.NumberType:
		buf := bytes.NewBuffer(nil)
		err := encode.Encode(node, buf)
		if err != nil {
			panic(err)
		}
		return "num-" + buf.String()
	case ir.StringType:
		return "str"
	case ir.BoolType:
		return strconv.FormatBool(node.Bool)
	case ir.NullType:
		return "null"
	default:
		panic("impossible")
	}
}
