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

// k8sFileName builds a filename from the k8s.filenames pattern.
// The pattern is a dash-separated list of tokens (name, kind, namespace).
func (fn *fileNamer) k8sFileName(pattern string) string {
	tokens := strings.Split(pattern, "-")
	parts := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		switch tok {
		case "name":
			n := fn.Metadata.Name
			if n == "" {
				n = "obj"
			}
			parts = append(parts, n)
		case "kind":
			k := strings.ToLower(fn.Kind)
			if k == "" {
				k = "unknown"
			}
			parts = append(parts, k)
		case "namespace":
			ns := fn.Metadata.Namespace
			if ns == "" {
				ns = "default"
			}
			parts = append(parts, ns)
		}
	}
	if len(parts) == 0 {
		return "obj"
	}
	return strings.Join(parts, "-")
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

func (d *Dir) fileName(node *ir.Node) string {
	// Check for explicit !filename(name) tag first
	if _, args := ir.TagGet(node.Tag, "!filename"); len(args) > 0 {
		return args[0]
	}

	switch node.Type {
	case ir.ObjectType:
		name := &fileNamer{}
		if err := gomap.FromTonyIR(node, name); err != nil {
			return "obj"
		}
		// Use k8s.filenames pattern if configured and this is a k8s object
		if d.Output != nil && d.Output.K8s != nil && d.Output.K8s.Filenames != "" &&
			name.Kind != "" {
			return name.k8sFileName(d.Output.K8s.Filenames)
		}
		return name.FileName()
	case ir.ArrayType:
		if len(node.Values) == 0 {
			return "arr"
		}
		return "arr-" + d.fileName(node.Values[0])
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
