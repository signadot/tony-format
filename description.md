# logd: a read answers null at a path nothing was ever written to

# logd: a read answers null at a path nothing was ever written to

**Any read that returns null should have a previous null write.** Today that does not hold: on a
store where nothing has been written, every path answers `Null` with no error — the root included.
So `null` means two things that cannot be told apart, and which one you get depends on whether some
*ancestor* happens to resolve.

```
FRESH STORE — nothing has ever been written:
  match root                                     -> Null
  match verse.a                                  -> Null
  match verse.a.b.c                              -> Null

AFTER writing verse.a.b.c = {x: 1}:
  match verse.a.b.c   (written)                  -> Object
  match verse.a.b.ZZZ (absent, ancestors resolve) -> not_found: no value at "verse.a.b.ZZZ":
                                                    resolved through "verse.a.b", no field "ZZZ"
  match verse.ZZZ     (absent, ancestors resolve) -> not_found: … no field "ZZZ"

AFTER writing verse.n = null (a null somebody DID write):
  match verse.n                                  -> Null
```

Line 1 and the last line are the same answer for opposite facts. `verse.a.b.ZZZ` and `verse.a`
are both "nothing was written here", and they answer differently — `not_found` in one case and
`Null` in the other — for a reason that is about the neighbourhood rather than about the path
asked for.

## The rule this asks for

> A read answers `null` **iff** a null was written at that path. Absence answers `not_found`, at
> every depth, whether or not an ancestor resolves.

Uniform, and it makes `null` mean one thing.

## Why the current behaviour is not just cosmetic

A client cannot recover the distinction from the answer, so every client either invents a
tiebreak or gets it wrong. Verse invents one: after a `Null` it asks the PARENT for the single
key (`MatchPattern` with `{<key>: null}`) and reads absence off whether the key came back. That
works — verified at every depth — but it is a second round trip, it is verse's guess at a rule
the store should state, and it is written down in verse as a comment explaining a substrate
behaviour rather than as a use of one:

> logd reports a missing key as not_found only when it can resolve a proper ancestor to report it
> against. Under a path whose ancestors do not resolve either, there is nothing to resolve through
> and the answer is a bare null: the same answer an entity holding null gives.

It also has a first-run shape. A fresh store answers `Null` at every path, so "the store is empty"
and "you asked for something that is not there" are indistinguishable on the read a client makes
first.

## Repro

Self-contained, no verse. Against `go-tony v0.0.200`.

```go
// go mod init nfrepro && go mod tidy && go run .
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/signadot/tony-format/go-tony/ir"
	docdserver "github.com/signadot/tony-format/go-tony/system/docd/server"
	"github.com/signadot/tony-format/go-tony/system/libctl"
	logdserver "github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func main() {
	dir, _ := os.MkdirTemp("", "nf")
	st, err := storage.Open(dir, nil)
	if err != nil {
		panic(err)
	}
	lg := logdserver.New(&logdserver.Spec{Storage: st})
	if err := lg.StartTCP("127.0.0.1:0"); err != nil {
		panic(err)
	}
	dc := docdserver.New(&docdserver.Spec{LogdAddr: lg.TCPAddr()})
	if err := dc.StartClientTCP("127.0.0.1:0"); err != nil {
		panic(err)
	}
	sess := libctl.NewLogdSession(&libctl.LogdSessionConfig{Addr: dc.ClientTCPAddr(), ClientID: "nf"})
	ctx := context.Background()
	if err := sess.Connect(ctx); err != nil {
		panic(err)
	}
	show := func(label, path string) {
		n, err := sess.Match(ctx, path)
		switch {
		case err != nil:
			fmt.Printf("  %-44s -> %v\n", label, err)
		case n == nil:
			fmt.Printf("  %-44s -> nil node, no error\n", label)
		default:
			fmt.Printf("  %-44s -> %v\n", label, n.Type)
		}
	}
	fmt.Println("FRESH STORE — nothing has ever been written:")
	show("match root", "verse")
	show("match verse.a", "verse.a")
	show("match verse.a.b.c", "verse.a.b.c")

	fmt.Println("\nAFTER writing verse.a.b.c = {x: 1}:")
	sess.Patch(ctx, "verse.a.b.c", ir.FromMap(map[string]*ir.Node{"x": ir.FromInt(1)}))
	show("match verse.a.b.c   (written)", "verse.a.b.c")
	show("match verse.a.b.ZZZ (absent, ancestors resolve)", "verse.a.b.ZZZ")
	show("match verse.ZZZ     (absent, ancestors resolve)", "verse.ZZZ")

	fmt.Println("\nAFTER writing verse.n = null (a null somebody DID write):")
	sess.Patch(ctx, "verse.n", ir.Null())
	show("match verse.n       (written as null)", "verse.n")
}
```

## Two neighbours, same principle, reported here rather than separately

Both are "a read answers a value nobody wrote", which is the same rule one step over. Whether they
belong in this fix is the maintainer's call; they are recorded so the fix can be uniform rather
than case-by-case.

**An emptied container reads as an empty object.** Delete the last member of `a.b` and `a.b`
answers `Object` with zero fields — indistinguishable from an `a.b` somebody explicitly wrote as
`{}`. The key stays in the parent, verified:

```
with a member:   a has 'b'? true    a.b has '1'? true
after deleting:  a has 'b'? true    a.b has '1'? false
```

So `{}` has the same ambiguity `null` has, one container up.

**A null written at the root empties the store**, and the after-state is a fresh store:

```
patch root <- null   ->  ok
  root now: Null      a previously written a.b.c now: not_found
```

That is coherent under "a patch replaces", and it is worth saying out loud next to this issue,
because it is the one write whose result is indistinguishable from never having written anything —
which is exactly what this issue is about.