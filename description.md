# codegen: a field-level comment= on an omitzero field inserts a nil into irMap and panics ToTonyIR

v0.0.145's field-level `comment=` emits its ApplyComments call OUTSIDE the omitzero guard, so a
field that is absent gets a nil written into the map under its key, and ir.FromMap dereferences
it.

Generated for `Gates []Gate ` + "`" + `tony:"field=gates,omitzero,comment=GatesDoc"` + "`":

    // Field: Gates
    if len(s.Gates) > 0 {
        slice := make([]*ir.Node, len(s.Gates))
        ...
        irMap["gates"] = ir.FromSlice(slice)
    }
    irMap["gates"] = gomap.ApplyComments(irMap["gates"], s.GatesDoc, nil)

With no gates, the guarded block does not run, so `irMap["gates"]` is missing; ApplyComments is
correct and returns its nil argument unchanged; the assignment then CREATES the key holding nil.

    panic: runtime error: invalid memory address or nil pointer dereference
    ir.FromMap(...) ir/node.go:158
    trigger.(*Trigger).ToTonyIR(...) trigger_gen.go:535

ir.FromMap does `y.Parent = res` over the values, so a nil value is a segfault rather than an
empty field.

In verse this crashes on any rule with no gates, which is most of them — an install panics in
persist. A required field is unaffected: the same annotation on `Condition` (no omitzero) works,
because the key is always there.

Either guard the call the way the field's own emission is guarded, or make it a no-op when the
key is absent:

    if _, ok := irMap["gates"]; ok {
        irMap["gates"] = gomap.ApplyComments(irMap["gates"], s.GatesDoc, nil)
    }

There is a design question hiding in it, which is why I have not guessed: a comment on a field
that is otherwise ABSENT has nowhere to go under omitzero — dropping it is defensible, and so is
emitting the field so the comment survives. Dropping is what the guard above does and is the
smaller change.