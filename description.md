# ir: Path() panics on a scalar parent, from inside an error message, so a refusal becomes a crash

ir.Node.Path() panics when a node's parent is a scalar, and it is called from
error messages -- so a patch that was merely going to be REFUSED kills the
process instead.

    ir/path.go:28   default: panic("parent but not in container")

The default arm is reached whenever Parent is neither Object, Array nor Comment.
Nothing in Path()'s contract says a node must be in a container: it is a display
path used to say WHERE something went wrong, and the one caller that reached it
here is an error string:

    mergeop/rename.go:88
      return nil, fmt.Errorf("cannot rename fields in non-object at %s of type %s",
        doc.Path(), doc.Type)

so the refusal never gets built.

Reproduction, through the store, deterministic:

    go test ./system/logd/storage -run TestAScopedWriteIsAStandingClaim
    (seed 3 of genClaimOps, op 22: a scoped `!rename [{from: "k2", to: "k2"}]`
     at path "a"; ops 0..21 in the log of that run)

    panic: parent but not in container
      ir.(*Node).Path
      mergeop.renameOp.Patch  rename.go:88
      tony.doPatchWith        patch.go:127
      tony.objMergeFast

At the tony level the same shapes are clean -- `{}` and `{a: 1}` patched with
`{a: !rename [...]}` both answer "cannot rename fields in non-object at $.a" --
so the node with the scalar parent is one the STORE built, and there are two
things here, either of which alone would have kept the process up:

  1. Path() should answer rather than panic. The Comment arm already handles "a
     parent that adds no step" by returning the parent's path; an unknown parent
     is the same situation with less information.

  2. Something in the read/fold path leaves a node whose Parent is a scalar.
     That is worth finding on its own -- Path() is not the only thing that walks
     parents.

Found by the claim-stability differential added with the scope-ownership work on
4wpqh7t2h12ks1fvj5n0; it is only reachable now because a scope may hold a
relative op at all.