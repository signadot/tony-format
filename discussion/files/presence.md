# Presence

Prerequisite 2 of wk5w1ddkh12krj1tkxn0. The issue states it as "a read answers 'there is
nothing here' as a value, not as a nil with a convention attached ... a type that makes the
two states unrepresentable as one". This document narrows that, and the narrowing is the
finding: THE NIL IS FINE. What was missing is that it was never uniform, and the one place a
type is actually needed is the wire, not the Go signature.

## The defect, exhibited

Absence is `(nil, nil)` today and each layer re-decides what it means.

LOWERING KEEPS THE TWO APART ON PURPOSE and says why (lower.go:212):

    // The write removed everything, and a diff of two STATES cannot say that:
    // the absent document is not the null one. Coercing next to null stored "the
    // document is null" for a write that said "there is no document" ...

THE WATCH COLLAPSES THEM, and it is one function (server/session.go:379):

    func subtreeOf(doc *ir.Node, path string) *ir.Node {
        if doc == nil { return ir.Null() }                      // absent
        sub, err := extractPathValue(doc, path)
        if err != nil { return ir.Null() }                      // absent
        if sub == nil || sub.Type == ir.NullType { return ir.Null() }   // absent, or null
        return sub
    }

Three returns of `ir.Null()` for two different facts. `stepBaseline` then gates on
`api.SameState(next, w.prev)`, which is `deepEqual` and answers false for nil against null --
so the gate would have told the truth. It never gets the chance, because its inputs were
flattened one call earlier. A watched path holding null that is then deleted delivers no
event, and the reason is these three lines and not the comparison.

THE WIRE CANNOT SAY IT EITHER, which is what forced the flattening. `sendInitialState` sends
`ir.Null()` for an absent path because a state event carries a value and no value means "there
is nothing here". `watchAbsence` (server/match_data.go:290) exists to fill that gap and can
only fill it in the LOG: it arms when a watch starts with no value and writes one line when a
real value arrives -- "A null subtree is not an arrival: it is how an absent path is
delivered". A convention attached to a nil, written down, doing its best.

## The rule, stated once

    A nil *ir.Node is ABSENT. Null is ir.Null(). No layer may map one onto the other.

That is the whole of property 2 at the Go boundary, and `(nil, nil)` is a perfectly good
spelling of it. It is already the idiom -- `GetPath` answers a missing field that way, and
`mergeop`'s `yKeyOf` documents it as deliberate. A type here would replace a uniform
convention with a uniform type and buy compile-time enforcement of something that has never
been the failing part.

What HAS failed is that the rule lived in three files as three comments. It belongs beside
`NextState` and `SameState` in api/state.go, which already argues exactly this for the wire
form: "nine copies of a convention is how a message loses something at one hop and nobody can
say which -- so the convention is written once, here".

## Where a type earns its place

TWO BOUNDARIES, AND NEITHER IS A GO RETURN.

1. THE WIRE. A state event must be able to say absent. Today it cannot, so it says null and
   logs the truth on the side. This is the one place the issue's "unrepresentable as one" is
   literally required, because the wire is where the distinction is currently destroyed and a
   client cannot recover it. Whether that is a field on the state event or a distinct event
   kind is a protocol question, and either makes the collapse in `subtreeOf` unnecessary
   rather than merely wrong.

2. THE THIRD STATE, which is the one that has actually produced wrong ANSWERS rather than
   missing events. Absent and null are two; "not determined" is the third. A keyed read that
   could not narrow used to answer "narrowed, absent" about an element that exists (c3e53a2).
   `(nil, nil)` must never be reachable from "I did not look".

   The rule is a rule about the error return, not about a presence type: A READ EITHER
   DETERMINES THE ANSWER OR ERRORS. Under read_write_interface.md a read always determines,
   because there is no narrowing that can decline -- so this state stops existing rather than
   needing to be represented. That is worth saying plainly: prerequisite 1 is what closes the
   only presence bug that ever returned a wrong value.

## It is already structural in three places, and stays so

THE EVENT STREAM. Zero events is absent; one null event is null. A cursor gets the
distinction for free, and `Cursor.Presence()` in the read interface is a name for what the
first `Next` would answer, not a second source of truth. It buffers one event to say it,
which is the term the bound already allows.

THE DELTA. A removal is a delete op; a null is a null value. `libdiff.MakeDiff(base, nil)` is
the root case lowering already relies on. Two rules follow and are worth stating as rules:
Diff never emits null for a removal, and Patch never turns a removal into a null.

THE INDEX. The spine proves a path was never written, which is absence established without
reading the log -- positive evidence rather than a nil arrived at by exhaustion. `AbsentSpineAt`
was an entry point for this; under the new interface it is what a cursor answers when the
index can prove it.

## What changes

  - `subtreeOf` stops flattening: absent is nil, and its three returns become one.
  - The state event says presence, and `watchAbsence` stops being how a client learns --
    it can stay as observability, but nothing depends on reading the log.
  - The rule is written once, in api/state.go, beside the two other decisions about how a
    store treats state.
  - Nothing else. There is no Presence type threaded through storage, because there is no
    defect that one would have caught.

## What is deliberately not decided

  - Whether the wire spells presence as a field on the state event or a distinct event kind.
  - Whether a client ever needs "not determined" as an answer rather than an error. Nothing
    needs it today and prerequisite 1 removes the case that produced it.
