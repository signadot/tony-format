# eval: a null written into a string is always "null"; an embedder cannot write it as ""

Context: verse expands a spawn's strings (prompts, task names, environment attributes) with `eval.ExpandStringWithOptions` (verse branch `verse/spawn-eval`, issue y1n12a3gh12ksh4mx5n0), under `EvalOptions{Composite: CompositeTony}`.

`anyToBytes` writes a nil as `null`, and nothing in `EvalOptions` says otherwise; `Composite` covers objects and lists only. In a prompt, `null` is a word that reads as content:

- a task's result keeps a null it wrote, so an onDone's `"Summary: $[data.summary]"` on `{summary: null}` reaches the agent as `Summary: null`;
- a completion hook binds `data` as null where the task wrote no document, so `"$[data]"` in a prompt an onDone and an onFailed share is `null` in the second;
- `get(obj, "absent")` is nil, so the lenient spelling of a key that is not a word writes `null` where the key is missing.

Verse wants these to be `""` in a string. It cannot get that on its side without changing the value: binding a null as `""` makes `data == nil` false and `??` useless, and `get` still answers nil. Rewriting each expression's text needs the scanner, which is unexported (y5n12a3gh12kraxfx5n0).

## Ask

An option for how a null is written into a string, alongside `Composite`, for example:

```go
// Null says how a null is written into a string: the word null, which is the
// default; nothing; or not at all. A string that is one .[...] is not written
// into anything, and keeps its value whatever this says.
Null NullForm

const (
	NullWord   NullForm = iota // null
	NullEmpty                  // ""
	NullRefuse                 // the expansion is an error
)
```

Scope: a null that IS an expression's value. A null inside a composite is written as `Composite` says (`{n: null}` stays so under `CompositeTony`).

Verse would set `Null: NullEmpty` beside `Composite: CompositeTony` in `launch.exprOptions`.