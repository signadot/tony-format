# eval: no strict mode for an embedder — absent and misspelt names render "null", `]` in an index closes the expression, `$[` has no escape, now() is reachable

Context: found while reviewing verse PR #70 (how a spawn's strings bind), against verse issue j28egpp2h12ksbzzp9n0, which proposes moving all of a charter's references (patches, spawn strings, gate poses) onto `$[…]` / `.[…]`. Verse's gate `pose` already uses `eval.ExpandString` (`trigger/fire.go`).

`$[…]` fixes verse's main problem with its own `$name` syntax. `$HOME`, `$5` and `$ENV_VAR` are never references, so nothing in the payload can capture them and no `$$` escape is needed. But an embedder that must refuse rather than render cannot use `ExpandString` as it stands. Verse's rule is that an absent reference fails the firing, and verse PR #70 extends that rule to spawns.

## Repro (go-tony v0.0.250)

`eval.ExpandString(s, env)`, where `env` is `{subject: {name: "a.b", value: {sev: high, n: 3, items: [1, 2], obj: {x: 1}}}}`:

| `s` | result | what an embedder needs |
|---|---|---|
| `$HOME and $5 and $ENV_VAR` | unchanged | ✓ |
| `$[subject.value.sev]` | `high` | ✓ |
| `$[subject.value.never]` | `null`, no error | an error: the field is absent |
| `$[nosuch]` | `null`, no error | an error: no such name (a typo) |
| `$[subject.value.never.x]` | error | ✓ |
| `$[subject.value.items[5]]` | `unexpected token EOF`: the index's `]` closes the expression | an index works unescaped; out of range is an error |
| `$[subject.value.obj]` | `{"x":1}` | the caller chooses: refuse, or render as tony |
| `$[now()]` | the current time | unreachable: a firing is retried and must expand the same way each time |
| `literal \$[x]` | `literal \null` | a way to write a literal `$[` outside an expression |

The closed issue 9mt3aenph12krmc1pdn0 added `fail()` and documented the absence rule. Null for a missing leaf under a present parent is the documented default, and `"x" in a` tells absent from null. This issue doesn't ask to change that default. It asks for an opt-in, so that an embedder doesn't have to require every author to write `"x" in a ? a.x : fail(…)` for every reference.

## Ask: an opt-in for embedders, as `EvalOptions`

1. **Strict lookups.** Under it, a missing field, a missing list index and an unknown top-level name are errors that name the path. Written null still resolves to null. `?.` and `??` keep their meaning, so a default stays possible where an author writes one. Unknown names could also be caught at compile time (`expr.Env` from the environment's keys).
2. **Bracket depth in the scanner.** `expandStringWithOptions` closes the expression at the first unescaped `]`. If it tracked `[`/`]` depth (and quoted strings), `items[5]` and `m["k"]` would work without `\]`. This one is arguably a fix, not an option: today `$[a[0]]` is a parse error for everyone.
3. **An escape outside expressions.** Today `$[` always starts an expression, and the only way to get a literal is `$["$["]`. `\$[` writes `\` followed by the expansion. Something like `$$[` → literal `$[` would give one rule an author can learn. (`$$` before anything other than `[` would stay as written, so `$$HOME` doesn't change.)
4. **The caller chooses the builtins.** Embedders can turn off the non-deterministic ones (`now`, `date`, `duration`?) or name an allow-list. `getenv`, `getpath` and `listpath` already stay out when there's no node, and that should remain true under the option.
5. **The caller chooses how a composite renders.** Interpolating an object or list into text is JSON today. Verse refuses that in a patch ("a composite has no text form") and renders it as tony in a prompt. An option for "refuse" or "tony" would let one embedder serve both.

Source: `eval/expand_env.go`: `expandStringWithOptions` (the scanner), `evalWithOptions` / `baseOpts` (compile options), `anyToBytes` (composite rendering).

## Why it matters downstream

Without 1, moving verse onto `$[…]` loses the refusal that verse PR #70 adds for a missing leaf. `$[subject.value.summary]` on a subject whose value has no `summary` would open a pull request whose body is `null`. (A missing parent is already refused: `$[subject.value.handoff.summary]` on a subject without `handoff` fails with `cannot fetch summary from <nil>`.) Without 2, every indexed path in a charter (`findings[0].severity`, common in verse) needs `\]`. 3, 4 and 5 are smaller, but each is a rule verse would otherwise have to state as "different from tony's".
