# mergeop: !field(from,to) keeps both fields when to exists and renames nothing when from does not, where !rename refuses both

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

    echo '{a: 1, b: 2}' | o p '!field(a,b) null'   ->  an object with two b keys
    echo '{a: 1}'       | o p '!field(z,y) null'   ->  exit 0, unchanged

!rename refuses both.

In logd:
- baseline (storage API): the stored delta is `a: !delete 1` and the state {b: 2} -- the
  renamed value is lost, silently;
- scope (live server): a scoped watcher receives `patch: !insert.raw {b: 1, b: 2}` -- a
  duplicate-key object in the log; a match in the same scope answers {b: 2};
- a missing from is stored as the operation itself, a no-op.

Consequence: a client write loses data at baseline and stores a shape the IR cannot hold
in a scope.