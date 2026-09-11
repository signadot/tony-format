# o: build -p writes to destDir and ignores -o, and -h promises a TONY_DIRBUILD_ENV nothing reads

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

- In a build dir with output.destDir, `o build -o out.tony .` writes 145 bytes to
  out.tony. Add `-p production`: out.tony is 0 bytes and 2 files land in output/.
  cmd/o/build.go:57 clears DestDir, then LoadProfile re-opens the Dir
  (dirbuild/profile.go:104-108) and restores it.
- `o build -h` promises TONY_DIRBUILD_ENV (cmd/o/commands.go:475, :515), but
  `TONY_DIRBUILD_ENV='{version: "2.0.0"}' o build -s` still shows version 1.0.0:
  dirbuild.LoadEnv has no callers.
- The same help text is garbled at commands.go:468, 473, 479, 491, 497
  ("build.{tony,objects ,json}", "object :", "patchs:", "my-pathes.tony").