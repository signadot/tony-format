package codegen

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// This file is about one thing the rest of the generator was not built for: a
// container inside a container.
//
// The field-level paths in generator.go descend exactly one level. They ask
// whether an element is a struct (call its codec) or a scalar (extract it), and
// a []T that is itself a []U is neither, so it reached generatePrimitiveToIR or
// generatePrimitiveFromIR and errored. That covers a field, an element, or a map
// value that is a scalar, a named scalar, a struct, a pointer to one, or a
// self-reference -- and nothing composed twice: [][]T, map[string][]T,
// []map[string]T, *map[string]T, *[]map[string]T.
//
// The emitters below are recursive on the type, so they bottom out at a scalar
// or a codec at whatever depth that happens, and nothing about them is specific
// to being an element rather than a field. They are called from the one-level
// paths only where those paths would otherwise fail, so the code generated for
// every shape that already worked is unchanged, byte for byte.
//
// The type EXPRESSION descends alongside the reflect.Type, because reflection
// cannot spell a type resolved from source: such a type arrives as an anonymous
// placeholder with no name. The expression comes from the AST (FieldInfo.
// GoTypeExpr) and is peeled one layer at a time by the helpers at the bottom of
// this file.

// isNestedContainer reports whether typ is something the one-level paths cannot
// emit code for: a slice, array or map, a pointer to one, or a pointer to a
// pointer.
//
// A pointer to a struct is deliberately not one of these -- it has a codec to
// call, the existing path calls it, and routing it here would rewrite generated
// code that is already correct. A pointer to a POINTER to a struct is, because
// there the existing path gives up.
func isNestedContainer(typ reflect.Type) bool {
	if typ == nil {
		return false
	}
	switch typ.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return true
	case reflect.Ptr:
		return typ.Elem().Kind() == reflect.Ptr || isNestedContainer(typ.Elem())
	}
	return false
}

// emitFromIR writes code that decodes the IR node named by src into dst, a Go
// expression of type typ spelled typeExpr. ctxExpr is a Go expression naming the
// position for error messages; errPrefix is what a failure reads as.
//
// depth keeps temporaries distinct: the same emitter runs at every level of a
// [][]map[string]T, and each level declares its own.
func emitFromIR(typ reflect.Type, typeExpr, src, dst, ctxExpr string, depth int, indent, currentPkgPath string) (string, error) {
	if typ == nil {
		return "", fmt.Errorf("cannot decode a field with no type information")
	}
	var buf strings.Builder
	i := indent
	v := func(name string) string { return fmt.Sprintf("%s%d", name, depth) }

	switch {
	case getIRNodeDepth(typ) > 0:
		// An *ir.Node holds the document fragment as it is.
		buf.WriteString(fmt.Sprintf("%s%s = %s\n", i, dst, src))

	case typ.Kind() == reflect.Interface:
		// any: hand the fragment to the reflection path, which reads whatever is
		// there. This is what the map-value case already does for interfaces.
		buf.WriteString(fmt.Sprintf("%sif err := gomap.FromTonyIR(%s, &%s); err != nil {\n", i, src, dst))
		buf.WriteString(fmt.Sprintf("%s	return fmt.Errorf(\"%%s: %%w\", %s, err)\n", i, ctxExpr))
		buf.WriteString(fmt.Sprintf("%s}\n", i))

	case typ.Kind() == reflect.Ptr && typ.Elem().Kind() == reflect.Struct:
		elem := v("p")
		buf.WriteString(fmt.Sprintf("%s%s := new(%s)\n", i, elem, strings.TrimPrefix(typeExpr, "*")))
		buf.WriteString(fmt.Sprintf("%sif err := %s.FromTonyIR(%s%s); err != nil {\n", i, elem, src, fromTonyIROptsSuffix(typ.Elem(), currentPkgPath)))
		buf.WriteString(fmt.Sprintf("%s	return fmt.Errorf(\"%%s: %%w\", %s, err)\n", i, ctxExpr))
		buf.WriteString(fmt.Sprintf("%s}\n", i))
		buf.WriteString(fmt.Sprintf("%s%s = %s\n", i, dst, elem))

	case typ.Kind() == reflect.Struct:
		elem := v("st")
		buf.WriteString(fmt.Sprintf("%s%s := %s{}\n", i, elem, typeExpr))
		buf.WriteString(fmt.Sprintf("%sif err := %s.FromTonyIR(%s%s); err != nil {\n", i, elem, src, fromTonyIROptsSuffix(typ, currentPkgPath)))
		buf.WriteString(fmt.Sprintf("%s	return fmt.Errorf(\"%%s: %%w\", %s, err)\n", i, ctxExpr))
		buf.WriteString(fmt.Sprintf("%s}\n", i))
		buf.WriteString(fmt.Sprintf("%s%s = %s\n", i, dst, elem))

	case typ.Kind() == reflect.Ptr:
		// Pointer to a container or a scalar. Null and a missing value both leave
		// it nil, which is how a pointer says "absent" as against "empty".
		inner := v("d")
		innerExpr := ptrElemExpr(typeExpr, typ.Elem(), currentPkgPath)
		buf.WriteString(fmt.Sprintf("%sif %s.Type != ir.NullType {\n", i, src))
		buf.WriteString(fmt.Sprintf("%s	var %s %s\n", i, inner, innerExpr))
		code, err := emitFromIR(typ.Elem(), innerExpr, src, inner, ctxExpr, depth+1, i+"	", currentPkgPath)
		if err != nil {
			return "", err
		}
		buf.WriteString(code)
		buf.WriteString(fmt.Sprintf("%s	%s = &%s\n", i, dst, inner))
		buf.WriteString(fmt.Sprintf("%s}\n", i))

	case arrayLenOf(typeExpr, typ) > 0:
		// A fixed-size array is not a slice: it cannot be made, and a document
		// with more elements than it holds is an error rather than a truncation,
		// which is the only reading that does not silently drop data. Fewer is
		// allowed -- the rest keep the element's zero value, as Go's own zero
		// array does.
		//
		// The length comes from the type expression because it cannot come from
		// reflection: the type resolver represents [2]string with a SLICE
		// placeholder, so Kind() answers Slice and Len() would panic.
		n := arrayLenOf(typeExpr, typ)
		arr, idx, elemVar := v("ar"), v("i"), v("e")
		elemExpr := sliceElemExpr(typeExpr, typ.Elem(), currentPkgPath)
		buf.WriteString(fmt.Sprintf("%sif %s.Type == ir.ArrayType {\n", i, src))
		buf.WriteString(fmt.Sprintf("%s	if len(%s.Values) > %d {\n", i, src, n))
		buf.WriteString(fmt.Sprintf("%s		return fmt.Errorf(\"%%s: %%d elements for %s\", %s, len(%s.Values))\n", i, typeExpr, ctxExpr, src))
		buf.WriteString(fmt.Sprintf("%s	}\n", i))
		buf.WriteString(fmt.Sprintf("%s	var %s %s\n", i, arr, typeExpr))
		buf.WriteString(fmt.Sprintf("%s	for %s, %s := range %s.Values {\n", i, idx, v("v"), src))
		buf.WriteString(fmt.Sprintf("%s		var %s %s\n", i, elemVar, elemExpr))
		elemCtx := fmt.Sprintf("fmt.Sprintf(\"%%s: element %%d\", %s, %s)", ctxExpr, idx)
		code, err := emitFromIR(typ.Elem(), elemExpr, v("v"), elemVar, elemCtx, depth+1, i+"		", currentPkgPath)
		if err != nil {
			return "", err
		}
		buf.WriteString(code)
		buf.WriteString(fmt.Sprintf("%s		%s[%s] = %s\n", i, arr, idx, elemVar))
		buf.WriteString(fmt.Sprintf("%s	}\n", i))
		buf.WriteString(fmt.Sprintf("%s	%s = %s\n", i, dst, arr))
		buf.WriteString(mismatch(i, src, "array", ctxExpr))

	case typ.Kind() == reflect.Slice:
		slice, idx, elemVar := v("sl"), v("i"), v("e")
		elemExpr := sliceElemExpr(typeExpr, typ.Elem(), currentPkgPath)
		buf.WriteString(fmt.Sprintf("%sif %s.Type == ir.ArrayType {\n", i, src))
		buf.WriteString(fmt.Sprintf("%s	%s := make([]%s, len(%s.Values))\n", i, slice, elemExpr, src))
		buf.WriteString(fmt.Sprintf("%s	for %s, %s := range %s.Values {\n", i, idx, v("v"), src))
		buf.WriteString(fmt.Sprintf("%s		var %s %s\n", i, elemVar, elemExpr))
		elemCtx := fmt.Sprintf("fmt.Sprintf(\"%%s: element %%d\", %s, %s)", ctxExpr, idx)
		code, err := emitFromIR(typ.Elem(), elemExpr, v("v"), elemVar, elemCtx, depth+1, i+"		", currentPkgPath)
		if err != nil {
			return "", err
		}
		buf.WriteString(code)
		buf.WriteString(fmt.Sprintf("%s		%s[%s] = %s\n", i, slice, idx, elemVar))
		buf.WriteString(fmt.Sprintf("%s	}\n", i))
		buf.WriteString(fmt.Sprintf("%s	%s = %s\n", i, dst, slice))
		buf.WriteString(mismatch(i, src, "array", ctxExpr))

	case typ.Kind() == reflect.Map && typ.Key().Kind() == reflect.Uint32:
		// A sparse array is an object whose keys are decimal indices.
		m, key, valVar := v("m"), v("k"), v("mv")
		valExpr := mapValueExpr(typeExpr, typ.Elem(), currentPkgPath)
		buf.WriteString(fmt.Sprintf("%sif %s.Type == ir.ObjectType {\n", i, src))
		buf.WriteString(fmt.Sprintf("%s	%s := make(map[uint32]%s)\n", i, m, valExpr))
		buf.WriteString(fmt.Sprintf("%s	for %sStr, %s := range ir.ToMap(%s) {\n", i, key, v("v"), src))
		buf.WriteString(fmt.Sprintf("%s		%s, err := strconv.ParseUint(%sStr, 10, 32)\n", i, key, key))
		buf.WriteString(fmt.Sprintf("%s		if err != nil {\n", i))
		buf.WriteString(fmt.Sprintf("%s			return fmt.Errorf(\"%%s: invalid sparse array key %%q: %%w\", %s, %sStr, err)\n", i, ctxExpr, key))
		buf.WriteString(fmt.Sprintf("%s		}\n", i))
		buf.WriteString(fmt.Sprintf("%s		var %s %s\n", i, valVar, valExpr))
		valCtx := fmt.Sprintf("fmt.Sprintf(\"%%s: key %%d\", %s, %s)", ctxExpr, key)
		code, err := emitFromIR(typ.Elem(), valExpr, v("v"), valVar, valCtx, depth+1, i+"		", currentPkgPath)
		if err != nil {
			return "", err
		}
		buf.WriteString(code)
		buf.WriteString(fmt.Sprintf("%s		%s[uint32(%s)] = %s\n", i, m, key, valVar))
		buf.WriteString(fmt.Sprintf("%s	}\n", i))
		buf.WriteString(fmt.Sprintf("%s	%s = %s\n", i, dst, m))
		buf.WriteString(mismatch(i, src, "object", ctxExpr))

	case typ.Kind() == reflect.Map && typ.Key().Kind() == reflect.String:
		m, key, valVar := v("m"), v("k"), v("mv")
		valExpr := mapValueExpr(typeExpr, typ.Elem(), currentPkgPath)
		// The key may be a NAMED string (type Key string), and then the map
		// cannot be indexed with a plain one. The name is in the expression; it
		// is not in the reflect.Type, which for a type resolved from source is an
		// anonymous placeholder.
		keyExpr := mapKeyExpr(typeExpr, typ.Key(), currentPkgPath)
		buf.WriteString(fmt.Sprintf("%sif %s.Type == ir.ObjectType {\n", i, src))
		buf.WriteString(fmt.Sprintf("%s	%s := make(map[%s]%s)\n", i, m, keyExpr, valExpr))
		buf.WriteString(fmt.Sprintf("%s	for %s, %s := range ir.ToMap(%s) {\n", i, key, v("v"), src))
		buf.WriteString(fmt.Sprintf("%s		var %s %s\n", i, valVar, valExpr))
		valCtx := fmt.Sprintf("fmt.Sprintf(\"%%s: key %%q\", %s, %s)", ctxExpr, key)
		code, err := emitFromIR(typ.Elem(), valExpr, v("v"), valVar, valCtx, depth+1, i+"		", currentPkgPath)
		if err != nil {
			return "", err
		}
		buf.WriteString(code)
		buf.WriteString(fmt.Sprintf("%s		%s[%s(%s)] = %s\n", i, m, keyExpr, key, valVar))
		buf.WriteString(fmt.Sprintf("%s	}\n", i))
		buf.WriteString(fmt.Sprintf("%s	%s = %s\n", i, dst, m))
		buf.WriteString(mismatch(i, src, "object", ctxExpr))

	case typ.Kind() == reflect.Map:
		// A tony object's keys are text, so a map's are a string or the uint32 of
		// a sparse array. The reflection path says this at runtime; codegen can
		// say it before there is a binary.
		return "", fmt.Errorf("unsupported map key type %s in %s: map keys must be strings or uint32", typ.Key(), typeExpr)

	default:
		// A scalar, at whatever depth the recursion reached one.
		scalar := v("x")
		code, err := generatePrimitiveFromIR(src, scalar, typ, ctxExpr)
		if err != nil {
			return "", fmt.Errorf("unsupported type %s: %w", typeExpr, err)
		}
		buf.WriteString(fmt.Sprintf("%svar %s %s\n", i, scalar, getQualifiedTypeName(typ, currentPkgPath)))
		buf.WriteString(indentBlock(code, i))
		buf.WriteString(fmt.Sprintf("%s%s = %s(%s)\n", i, dst, typeExpr, scalar))
	}

	return buf.String(), nil
}

// nestedFieldToIR writes the encoding of a field whose type nests containers,
// which is where generateFieldToIR's one-level cases give out.
//
// The wrapper around the recursion is the field's, not the value's: a nil
// pointer field is omitted entirely rather than written as null, because that is
// what every other optional field does and what makes a pointer's nil mean
// "absent" on the wire; omitzero on a bare container means the same for empty.
func nestedFieldToIR(field *FieldInfo, schemaFieldName, currentPkgPath string) (string, error) {
	var buf strings.Builder
	typ := field.Type
	typeExpr := getFieldTypeName(field, currentPkgPath)
	src := "s." + field.Name

	if typ.Kind() == reflect.Ptr {
		buf.WriteString(fmt.Sprintf("	if s.%s != nil {\n", field.Name))
		src = "(*s." + field.Name + ")"
		typeExpr = ptrElemExpr(typeExpr, typ.Elem(), currentPkgPath)
		typ = typ.Elem()
	} else {
		buf.WriteString(collectionGuardOpen(field))
	}

	code, err := emitToIR(typ, typeExpr, src, "node", 0, "		", currentPkgPath)
	if err != nil {
		return "", err
	}
	buf.WriteString("		var node *ir.Node\n")
	buf.WriteString(code)
	buf.WriteString(fmt.Sprintf("		irMap[%q] = node\n", schemaFieldName))
	buf.WriteString("	}\n")
	return buf.String(), nil
}

// mismatch closes a container's type guard with an else that refuses the node,
// naming the type the field wanted and the one the document had.
//
// The alternative -- close the guard and carry on -- is what the one-level paths
// do, and it is wrong here in a way it is not there. Those leave a plain field
// at its zero value, which reads as absent, so nothing is asserted. A pointer
// field's decoder assigns the pointer AFTER this code, so a skipped decode would
// leave a non-nil pointer to a nil slice: "the author wrote an empty list" for a
// document whose author wrote "hello". That is the one reading the pointer
// exists to make, invented from a type error. gomap's reflection path already
// errors on the same document ("expected array, got String"), and the two paths
// must not disagree about what a document means.
func mismatch(indent, src, want, ctxExpr string) string {
	return fmt.Sprintf("%s} else {\n%s	return fmt.Errorf(\"%%s: expected %s, got %%v\", %s, %s.Type)\n%s}\n",
		indent, indent, want, ctxExpr, src, indent)
}

// emitToIR writes code that converts src, a Go expression of type typ spelled
// typeExpr, into an *ir.Node assigned to dst.
func emitToIR(typ reflect.Type, typeExpr, src, dst string, depth int, indent, currentPkgPath string) (string, error) {
	if typ == nil {
		return "", fmt.Errorf("cannot encode a field with no type information")
	}
	var buf strings.Builder
	i := indent
	v := func(name string) string { return fmt.Sprintf("%s%d", name, depth) }

	switch {
	case getIRNodeDepth(typ) > 0:
		derefs := strings.Repeat("*", getIRNodeDepth(typ)-1)
		buf.WriteString(fmt.Sprintf("%s%s = %s%s\n", i, dst, derefs, src))

	case typ.Kind() == reflect.Interface:
		node := v("n")
		buf.WriteString(fmt.Sprintf("%s%s, err := gomap.ToTonyIR(%s, opts...)\n", i, node, src))
		buf.WriteString(fmt.Sprintf("%sif err != nil {\n", i))
		buf.WriteString(fmt.Sprintf("%s	return nil, err\n", i))
		buf.WriteString(fmt.Sprintf("%s}\n", i))
		buf.WriteString(fmt.Sprintf("%s%s = %s\n", i, dst, node))

	case typ.Kind() == reflect.Struct || (typ.Kind() == reflect.Ptr && typ.Elem().Kind() == reflect.Struct):
		target := typ
		if typ.Kind() == reflect.Ptr {
			target = typ.Elem()
		}
		node := v("n")
		buf.WriteString(fmt.Sprintf("%s%s, err := %s.ToTonyIR(%s)\n", i, node, src, toTonyIROptsSuffix(target, currentPkgPath)))
		buf.WriteString(fmt.Sprintf("%sif err != nil {\n", i))
		buf.WriteString(fmt.Sprintf("%s	return nil, err\n", i))
		buf.WriteString(fmt.Sprintf("%s}\n", i))
		buf.WriteString(fmt.Sprintf("%s%s = %s\n", i, dst, node))

	case typ.Kind() == reflect.Ptr:
		// A nil pointer is null on the wire; the reader turns it back into nil.
		inner := v("pv")
		innerExpr := ptrElemExpr(typeExpr, typ.Elem(), currentPkgPath)
		buf.WriteString(fmt.Sprintf("%sif %s == nil {\n", i, src))
		buf.WriteString(fmt.Sprintf("%s	%s = ir.Null()\n", i, dst))
		buf.WriteString(fmt.Sprintf("%s} else {\n", i))
		buf.WriteString(fmt.Sprintf("%s	%s := *%s\n", i, inner, src))
		code, err := emitToIR(typ.Elem(), innerExpr, inner, dst, depth+1, i+"	", currentPkgPath)
		if err != nil {
			return "", err
		}
		buf.WriteString(code)
		buf.WriteString(fmt.Sprintf("%s}\n", i))

	case typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array:
		nodes, idx, elemNode := v("ns"), v("i"), v("en")
		elemExpr := sliceElemExpr(typeExpr, typ.Elem(), currentPkgPath)
		buf.WriteString(fmt.Sprintf("%s%s := make([]*ir.Node, len(%s))\n", i, nodes, src))
		buf.WriteString(fmt.Sprintf("%sfor %s, %s := range %s {\n", i, idx, v("e"), src))
		buf.WriteString(fmt.Sprintf("%s	var %s *ir.Node\n", i, elemNode))
		code, err := emitToIR(typ.Elem(), elemExpr, v("e"), elemNode, depth+1, i+"	", currentPkgPath)
		if err != nil {
			return "", err
		}
		buf.WriteString(code)
		buf.WriteString(fmt.Sprintf("%s	%s[%s] = %s\n", i, nodes, idx, elemNode))
		buf.WriteString(fmt.Sprintf("%s}\n", i))
		buf.WriteString(fmt.Sprintf("%s%s = ir.FromSlice(%s)\n", i, dst, nodes))

	case typ.Kind() == reflect.Map && typ.Key().Kind() == reflect.Uint32:
		nodes, valNode := v("ns"), v("vn")
		valExpr := mapValueExpr(typeExpr, typ.Elem(), currentPkgPath)
		buf.WriteString(fmt.Sprintf("%s%s := make(map[uint32]*ir.Node)\n", i, nodes))
		buf.WriteString(fmt.Sprintf("%sfor %s, %s := range %s {\n", i, v("k"), v("e"), src))
		buf.WriteString(fmt.Sprintf("%s	var %s *ir.Node\n", i, valNode))
		code, err := emitToIR(typ.Elem(), valExpr, v("e"), valNode, depth+1, i+"	", currentPkgPath)
		if err != nil {
			return "", err
		}
		buf.WriteString(code)
		buf.WriteString(fmt.Sprintf("%s	%s[%s] = %s\n", i, nodes, v("k"), valNode))
		buf.WriteString(fmt.Sprintf("%s}\n", i))
		buf.WriteString(fmt.Sprintf("%s%s = ir.FromIntKeysMap(%s)\n", i, dst, nodes))

	case typ.Kind() == reflect.Map && typ.Key().Kind() == reflect.String:
		nodes, valNode := v("ns"), v("vn")
		valExpr := mapValueExpr(typeExpr, typ.Elem(), currentPkgPath)
		buf.WriteString(fmt.Sprintf("%s%s := make(map[string]*ir.Node)\n", i, nodes))
		// string(k) below converts a named key type; it is an identity no-op for
		// a plain string key.
		buf.WriteString(fmt.Sprintf("%sfor %s, %s := range %s {\n", i, v("k"), v("e"), src))
		buf.WriteString(fmt.Sprintf("%s	var %s *ir.Node\n", i, valNode))
		code, err := emitToIR(typ.Elem(), valExpr, v("e"), valNode, depth+1, i+"	", currentPkgPath)
		if err != nil {
			return "", err
		}
		buf.WriteString(code)
		buf.WriteString(fmt.Sprintf("%s	%s[string(%s)] = %s\n", i, nodes, v("k"), valNode))
		buf.WriteString(fmt.Sprintf("%s}\n", i))
		buf.WriteString(fmt.Sprintf("%s%s = ir.FromMap(%s)\n", i, dst, nodes))

	case typ.Kind() == reflect.Map:
		return "", fmt.Errorf("unsupported map key type %s in %s: map keys must be strings or uint32", typ.Key(), typeExpr)

	default:
		code, err := generatePrimitiveToIR(src, typ)
		if err != nil {
			return "", fmt.Errorf("unsupported type %s: %w", typeExpr, err)
		}
		buf.WriteString(fmt.Sprintf("%s%s = %s\n", i, dst, code))
	}

	return buf.String(), nil
}

// indentBlock re-indents a block of generated lines, which arrive from helpers
// written for one nesting level and are used at several.
func indentBlock(code, indent string) string {
	var out strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(code, "\n"), "\n") {
		out.WriteString(indent + strings.TrimLeft(line, "\t") + "\n")
	}
	return out.String()
}

// sliceElemExpr peels "[]" (or "[N]") off a slice's type expression. The
// fallback is for a FieldInfo built without an AST behind it, which is a test,
// and for which reflection is enough.
func sliceElemExpr(typeExpr string, elemType reflect.Type, currentPkg string) string {
	if rest, ok := strings.CutPrefix(typeExpr, "[]"); ok && rest != "" {
		return rest
	}
	if strings.HasPrefix(typeExpr, "[") {
		if _, rest, ok := strings.Cut(typeExpr, "]"); ok && rest != "" {
			return rest
		}
	}
	return getQualifiedTypeName(elemType, currentPkg)
}

// mapValueExpr peels "map[K]" off a map's type expression. The key cannot itself
// be a composite -- a map key is a scalar or a named one -- so the first "]"
// closes the key.
func mapValueExpr(typeExpr string, valueType reflect.Type, currentPkg string) string {
	if rest, ok := strings.CutPrefix(typeExpr, "map["); ok {
		if _, val, found := strings.Cut(rest, "]"); found && val != "" {
			return val
		}
	}
	return getQualifiedTypeName(valueType, currentPkg)
}

// arrayLenOf answers the length of a fixed-size array, or 0 for anything else.
//
// The type expression is the authority, not reflection: the type resolver builds
// [2]string as a SLICE placeholder, so Kind() answers Slice and Len() panics.
// A FieldInfo built by hand in a test has no expression but does have a real
// reflect.Type, so that is the fallback.
func arrayLenOf(typeExpr string, typ reflect.Type) int {
	if rest, ok := strings.CutPrefix(typeExpr, "["); ok {
		if n, _, found := strings.Cut(rest, "]"); found && n != "" {
			if length, err := strconv.Atoi(n); err == nil && length > 0 {
				return length
			}
		}
		return 0
	}
	if typ != nil && typ.Kind() == reflect.Array {
		return typ.Len()
	}
	return 0
}

// isArrayExpr reports whether a field's type expression is a fixed-size array,
// which the one-level paths would emit a slice for.
func isArrayExpr(typeExpr string, typ reflect.Type) bool {
	return arrayLenOf(typeExpr, typ) > 0
}

// mapKeyExpr answers a map's key type as the generated file must spell it,
// which is "string" for the common case and the declared name for a named key
// (type Key string), since a map[Key]V cannot be indexed with a plain string.
func mapKeyExpr(typeExpr string, keyType reflect.Type, currentPkg string) string {
	if rest, ok := strings.CutPrefix(typeExpr, "map["); ok {
		if key, _, found := strings.Cut(rest, "]"); found && key != "" {
			return key
		}
	}
	return getQualifiedTypeName(keyType, currentPkg)
}

// hasNamedMapKey reports whether a map field's key is a named string type, which
// the one-level paths spell as "string" and so generate code that does not
// compile.
func hasNamedMapKey(typ reflect.Type, typeExpr string, currentPkg string) bool {
	if typ == nil || typ.Kind() != reflect.Map || typ.Key().Kind() != reflect.String {
		return false
	}
	return mapKeyExpr(typeExpr, typ.Key(), currentPkg) != "string"
}

// ptrElemExpr peels "*" off a pointer's type expression.
func ptrElemExpr(typeExpr string, elemType reflect.Type, currentPkg string) string {
	if rest, ok := strings.CutPrefix(typeExpr, "*"); ok && rest != "" {
		return rest
	}
	return getQualifiedTypeName(elemType, currentPkg)
}
