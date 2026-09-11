# tony-codegen: -recursive processes only the first package, because every directory's go/build import path is "."

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

`tony-codegen -dir v9b -recursive` on a tree holding packages p1 and p2 generates for p1
only, and exits 0.

DiscoverPackages(dir, true) dedups on build.ImportDir's ImportPath
(gomap/codegen/discovery.go:44, 56-59), which is "." for every directory outside GOPATH
-- so every package after the first is a duplicate.

Consequence: docs/gomap.md:715 documents `tony-codegen -recursive -dir ./...`, and it
silently skips all but one package.