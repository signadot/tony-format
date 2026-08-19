# docd: a composed watch delivers each commit once per overlapping sub-stream

Found while testing composed-watch replay (4ses3fqsh12ks8awgnn0); it is not caused by that
work -- live streaming does it too.

A composed watch on a path with mounts below it starts a sub-watch on the base owner AND one
on each mount. A logd-backed mount writes to logd, so the base sub-watch (which watches the
composed path in logd) sees the mount's commits as well as its own. Both forward, so the
client gets the same delta once per overlapping sub-stream.

MEASURED, one mount under the watched path (libctl.TestComposedWatchLiveDuplication):

    watch "verse", mount at "verse.a", ONE write to verse.a.one
    live deltas per commit: map[2:2]        <- commit 2 delivered twice

and the same on replay (TestComposedWatchResumesFromACommit), where a four-commit window
delivered eight deltas: map[3:2 4:2 5:2 6:2].

WHY IT MATTERS. A merge patch applied twice is usually harmless, which is presumably why
this has not bitten yet. An OPERATION is not: !arraydiff with an insert, or any op whose
result depends on what it meets, applied twice is wrong -- and preserving op fidelity is
exactly why the composed path forwards the raw committed delta rather than a re-wrapped
diff (see forward()).

WHAT THE FIX PROBABLY IS. The base sub-watch of a composed watch should not carry what a
mount below it owns -- either by watching the base with the mounted subtrees excluded (no
such watch shape exists today), or by docd dropping a delta from the base stream whose path
falls under a mount it also watches, or by deduplicating on (commit, sub-path) before
forwarding. The last is the cheapest and the least principled.

Whatever the shape, the test to keep is the measurement above: one write, one delta.