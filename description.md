# patches: the fold refuses to graft an object write into an unkeyed array where tony.Patch replaces it, so a scoped read fails after baseline turns a path the scope wrote under into an array of objects

Found by the scope differential's new shapes (scope_plan.md, phase 0); pre-existing and not
about scopes as such, but a scope is how the failing ORDER arises.

## The failure

    scope     k2.n  <- !delete
    baseline  ""    <- {k2: [{n: 21}]}
    scoped read at the root:
      failed to apply patches: cannot graft [n] into the array at "k2": creating array
      elements needs the key field

Every scoped read at or above k2 fails from then on (TestLoweringScopeDifferential, seed
13, op 21, on the generator before it was steered around this). The fold applies the
scope's entries after baseline's, so the scope's `{k2: {n: !delete}}` meets an array at k2
and the streaming processor's graft refuses to put a FIELD into an unkeyed array
(internal/patches/processor.go, graftUpTo, "creating array elements needs the key field").

## What tony.Patch does with the same pair

    [{n: 21}]  <-  {n: !delete}    =>  {}
    [{n: 1}]   <-  {n: 5}          =>  {n: 5}

An object patch over an array REPLACES it (doPatchWith switches on the patch; absentAt says
"replacing a scalar is what an object patch over one does", and an array is no different
here). So the reference semantics have an answer and the fold has a refusal: the two
disagree, and the disagreement is a read that fails rather than a read that differs.

In baseline alone the order that reaches it is rare -- the object write lands first and the
array replaces it, which the fold handles -- which is why this stayed hidden until a scope's
apply-last ordering put the object write after the array.

## Two possible answers, and it should be one of them on purpose

  - The fold does what tony.Patch does: grafting a field path into an array replaces the
    array with an object and continues. Reads stop failing and agree with the reference.
  - The write is refused: a field write below a path that holds an unkeyed array is a write
    into a VALUE (element_identity.md: an array that declares no identity is a value, the
    whole array is the unit), and the verification at the site cannot see it today because
    it reads the SITE (k2.n, absent) and not the spine the write descends through. That is
    the guard verse keeps for itself (descendsThrough) and the store does not.

The first is a processor change, below the line; the second is a lowering change. Either
way the differential's generator should get its array shapes back onto the shared keys once
this is decided (lower_scope_test.go, genScopeOps).