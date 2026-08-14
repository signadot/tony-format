# o: an exec-less `!convert-image` the consumer opts into, and the unknown-op tag that reads as data rather than failing

A consumer of `o` who rewrites image references today writes `!pipe "convert-image -suffix -test"`,
which means their deployment has to account for a second binary: `go-tony/Dockerfile` copies
`convert-image` to `/usr/local/bin` beside `o` for no reason other than making that `!pipe` resolve,
and outside the image it has to be on PATH. The work `convert-image` does is 30 lines of ref parsing
with no reason to be a process at all.

So: an exec-less `!convert-image {registry: ..., repo: ..., suffix: ..., tag: ...}` merge op, which the
consumer opts into. `!pipe` stays exactly as it is, registered and unsafe -- this removes a binary from
a deployment, not an escape hatch from the format.

## The hazard the design exists to avoid

An unknown op tag is not an error. `mergeop.SplitChild` (mergeop/split_child.go:21) folds a tag it
cannot look up into `preTag` and carries it as data:

	if Lookup(tag) == nil {
		preTag = ir.TagCompose("!"+tag, args, preTag)
		tag = rest
		continue
	}

That is right for a document that holds `!my-annotation` as data. It is wrong for an op that is opt-in,
because the two cases are indistinguishable at the point they are read. A document written against
`!convert-image` and run by an `o` that did not enable it does not fail: the node passes through, the
tag rides along as data, and the build emits the un-rewritten image. The consumer trades an install
step for a silently wrong output, which is the worse of the two by a distance -- a missing binary at
least says so.

Whatever the opt-in mechanism, `o` therefore has to distinguish a tag it has never heard of from an op
it was built with and was not asked to enable. It can: the optional ops are compiled in, so a catalog
of them is available to consult before the document is read. A tag naming one that is not enabled must
fail, and say what to do:

	!convert-image is available but not enabled; add --op convert-image (or ops: in build.tony)

Without that, this issue makes things worse rather than better, so it is the part to get right first.

## Where the opt-in lives

The consumer here does not use the Go library, only the `o` binary, so `mergeop.Register` in someone's
main is not a mechanism they can reach. Two seams, both wanted:

- `o --op 'convert-image {repo: signadot, tag: latest}'`, repeatable. Option values are already parsed
  into structured things through `cli.FuncOpt` (see `MainConfig.fmtFunc` and `outOpt` in
  cmd/o/configs.go), so the value can be the op name plus its registration-time defaults as tony. This
  is the general seam: it works in oio mode and dir mode alike.
- An `ops:` field on `dirbuild.Dir` (dirbuild/dir.go:25), beside `sources`, `patches` and `env`, and
  schemagen'd with them. This is the explicit config a consumer commits, and it travels with the
  documents that need it. It covers dir mode only, which is why the flag is not optional.

A `TONY_OPS` environment variable, matching `TONY_DIRBUILD_ENV` (dirbuild/load_env.go:14), would be a
reasonable CI fallback and a bad primary -- too implicit to be the thing a reader of the build finds.

Defaults come from the registration -- the flag or the build file -- and a document's mapping overrides
them per node, so a bare `!convert-image {}` means "whatever this binary was configured to do". That is
where `repo: signadot` belongs: it is one consumer's policy, and it has no business in a general
library shipped to everyone.

## Notes on implementing it

Nothing in the core has to change. `o` is the caller: it can register into the existing global registry
from `main` once its flags and build file are parsed. `mergeop.Symbol`, `Name` and `Op` are exported
interfaces whose methods are all exported, so an op can be defined outside the package as it is; the
one gap is that `patchName`/`matchName` are unexported, so an outside implementation writes
`String`/`IsMatch`/`IsPatch` by hand. Exporting a helper for that is small and worth doing here.

The op itself is the shape of `!retag` (mergeop/retag.go): an `Instance` that reads the mapping and a
`Patch` on a string node. The ref parsing is `novln/docker-parser`, already in go.mod and imported
today by nothing but cmd/convert-image. Put it in its own package -- `go-tony/imageref` -- with both
the op and the `convert-image` main over it, so the two entry points cannot drift.

Being exec-less, the op is a pure function of the node it meets, which means unlike `!pipe` it is
storable: it does not have to be excluded from logd's storage vocabulary
(system/logd/api/storage_context.go), and it survives `RejectUnsafe`.

## Migration

Nothing has to move at once, which is the point of keeping `!pipe`. Existing documents saying
`!pipe "convert-image ..."` keep working wherever the binary is installed. Documents move to
`!convert-image` when their consumer enables it. The `COPY` in go-tony/Dockerfile drops when the last
one has moved, and not before.

## Not in scope

Removing `!pipe`, changing `RejectUnsafe`, or making the mergeop registry a per-caller value. The
registry is a process global and that is a real constraint -- `libdiff.IsOp` is wired to it, so what
counts as an op tag is a process-wide fact -- but a CLI that registers once at startup does not feel
it, and this issue does not need it.