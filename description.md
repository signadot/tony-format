# schema: base.tony's key(p) uses !all.hasPath, which is not an operator, so any schema using .[key(...)] fails to load

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53 with the
real `o`.

schema/base.tony:51 (embedded) and schema/schema.tony:55 (loaded by nothing) write
`!all.hasPath`; the registered operator is `has-path` (mergeop/has_path.go:17).

`o schema check` on a schema with `accept: .[key(name)]` exits 2:

    error building formula for definition "key(p)": unknown tag in schema: !hasPath

As a plain match, `!all.hasPath name` matches nothing, not even [{name: a}].

Consequence: any schema using .[key(...)] fails to load in `o schema check`.