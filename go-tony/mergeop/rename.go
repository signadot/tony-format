package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var renameSym = &renameSymbol{patchName: renameName}

// Rename is the !rename operator: the operand is a list of {from, to} pairs, and
// each field of the object named by a from: is renamed to its to:.
//
//	!rename
//	- from: spec
//	  to: sp
//
// The pairs are SIMULTANEOUS. They are a list of statements about one object,
// not a program, so each is read against the document as it stands and all of
// them are installed together: `[{from: a, to: b}, {from: b, to: a}]` swaps the
// two, and the result does not depend on the order the pairs are written in.
//
// A from: which names no field renames nothing -- the operation is relative to
// the keys that are there -- while a to: which collides with a field that is
// still there is refused, since one of the two would have to be lost.
//
// !field(from,to) is the same operation for a single field, written on the field
// rather than on the object holding it.
func Rename() Symbol {
	return renameSym
}

const (
	renameName patchName = "rename"
)

type renameSymbol struct {
	patchName
}

type renaming struct {
	from, to string
}

func (s renameSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	if child.Type != ir.ArrayType {
		return nil, fmt.Errorf("rename must be applied to an array ")
	}
	renamings := make([]renaming, 0, len(child.Values))
	for _, v := range child.Values {
		from := ir.Get(v, "from")
		if from == nil {
			return nil, fmt.Errorf("renaming missing from at %s", child.Path())
		}
		if from.Type != ir.StringType {
			return nil, fmt.Errorf("renaming from should be string at %s", from.Path())
		}
		to := ir.Get(v, "to")
		if to == nil {
			return nil, fmt.Errorf("renaming missing to at %s", child.Path())
		}
		if to.Type != ir.StringType {
			return nil, fmt.Errorf("renaming to should be string at %s", to.Path())
		}
		renamings = append(renamings, renaming{from: from.String, to: to.String})
	}
	return &renameOp{
		renamings: renamings,
		patchOp:   patchOp{op: op{name: s.patchName, child: child}},
	}, nil
}

type renameOp struct {
	patchOp
	renamings []renaming
}

func (p renameOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("rename op patch on %s\n", doc.Path())
	}
	if doc.Type != ir.ObjectType {
		return nil, fmt.Errorf("cannot rename fields in non-object at %s of type %s", doc.Path(), doc.Type)
	}
	to := make(map[string]string, len(p.renamings))
	for i := range p.renamings {
		r := &p.renamings[i]
		if was, twice := to[r.from]; twice {
			return nil, fmt.Errorf(
				"!%s at %s: %q is renamed twice, to %q and to %q",
				p.name, doc.Path(), r.from, was, r.to)
		}
		to[r.from] = r.to
	}
	// The document's own fields, walked once and renamed where they are, rather
	// than a map keyed by name.
	//
	// Once, because that is what makes the pairs simultaneous: a field is read
	// under the name it arrived with, so a swap is a swap and no renaming can
	// see another's result. Where they are, because a map cannot hold what an
	// object holds. ir.ToMap skips a null-typed key and ir.FromMap cannot put
	// one back, so going through one DELETED every merge key in the object --
	// `<<: "{{ tpl }}"` alongside a renamed field simply vanished -- and it
	// collapsed a non-string key onto "" rather than leaving it alone.
	//
	// Nothing was moving before this: the value was copied to the new name and
	// the old field left in place, so `!rename [{from: spec, to: sp}]` -- the
	// example in the operator's own documentation -- answered with both spec
	// and sp.
	kvs := make([]ir.KeyVal, 0, len(doc.Fields))
	held := make(map[string]int, len(doc.Fields))
	for i := range doc.Fields {
		field := doc.Fields[i]
		key := field.Clone()
		// A merge key names no field, and neither does an integer one, so no
		// renaming names either: they stay as they are.
		if field.Type == ir.StringType {
			if renamed, ok := to[field.String]; ok {
				key.String = renamed
			}
			if prev, taken := held[key.String]; taken {
				return nil, fmt.Errorf(
					"!%s at %s: %q and %q would both be %q, and one of them would be lost",
					p.name, doc.Path(), doc.Fields[prev].String, field.String, key.String)
			}
			held[key.String] = i
		}
		kvs = append(kvs, ir.KeyVal{Key: key, Val: doc.Values[i]})
	}
	return ir.FromKeyVals(kvs).WithTag(doc.Tag), nil
}
