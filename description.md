# gomap: Mapper cannot read the schemas tony-codegen writes, and errors, drops options or mis-dispatches where gomap.ToTonyIR does not

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); each probed at 546bd53. Mapper
has no production callers; this is the library surface.

- It cannot read codegen's spellings: ExtractGoType(".[array(string)]") fails with
  `definition "array(string)" not found in schema` (".array(string)" works), likewise
  .[nullable(string)], and IsNullable(".[nullable(string)]") is false. A schema= Mapper
  over a codegen-written schema fails: `failed to get struct fields: ... not found`.
  type_extract.go handles only .name(args) (:622); the .[...] branches (:52, :137) take
  array(string) as a plain definition name.
- It errors instead of falling back: GetStructSchema returns an error, not nil, for an
  unmarked type (tags.go:403) and mapper_to.go:38 passes it on, so the reflection
  fallback at :49 never runs -- Mapper.ToTonyIR(Plain{}) fails "no schema tag found",
  ToTonyIR(&Plain{}) and ToTonyIR(3) "expected struct type". Its doc promises the
  fallback.
- FromTonyIR's fallback (mapper_from.go:55) runs for a marked struct with a
  head-commented document and drops opts: Strict is ignored.
- It dispatches to a codec PROMOTED from an embedded field and drops the struct's own
  fields (no declaresMethod check, mapper_to.go:22, mapper_from.go:32): {x: 1} where
  gomap.ToTonyIR gives {b: 2, x: 1}.
- It tags a schema= struct with !name even when the marker says notag (mapper_to.go:108).