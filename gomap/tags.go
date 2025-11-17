package gomap

import (
	"reflect"

	"github.com/signadot/tony-format/tony/ir"
)

func fillTags(node *ir.Node, p any) error {
	ty := reflect.TypeOf(p)
	if ty == nil {
		return nil
	}
	val := reflect.ValueOf(p)
	switch ty.Kind() {
	case reflect.Struct:
	case reflect.Pointer:
		val = val.Elem()
		ty = ty.Elem()
		if ty.Kind() != reflect.Struct {
			return nil
		}
	default:
		return nil
	}
	n := ty.NumField()
	var opts []*Opt
	for i := range n {
		f := ty.Field(i)
		if f.Anonymous {
			// todo tag nesting
			continue
		}
		err := tagOpt(f.Tag.Get("tony"), fVal, node)
		if err != nil {
			return err
		}
	}
	return nil
}

func tagOpt(tag string, fVal reflect.Value, node *ir.Node) error {
	fVal.Interface()
	fVal.Set
	return nil
}
