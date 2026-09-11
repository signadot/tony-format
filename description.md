# tony-codegen: -schema-dir keeps only the package directory's base name, so packages a/x and b/x overwrite one schema file

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

`-schema-dir` says it "preserves package structure" (cmd/tony-codegen/main.go:46), but
:251 keeps only filepath.Base(pkg.Dir). Running with -schema-dir on a/x and then b/x
leaves one file, x/schema_gen.tony, holding only b's schema.

Consequence: two packages with the same directory name overwrite each other's schemas,
silently.