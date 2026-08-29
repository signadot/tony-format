# logd: a document-root comment is lost from a scoped read once a later scoped write states a field beneath it

A head comment stored by one scoped write disappears from the scoped read once a
later scoped write claims a sibling path.

    seed    baseline   {a: {z: 9}}
    op 1    scope a <- "# note\n{k0: 7}"
    op 2    scope a <- !rename [{from: "z", to: "y"}]

    scoped read   a: { k0: 7 y: 9 }        the # note is gone

The log still holds it, so nothing dropped it on the way in -- the two entries
are:

    scope entry@2:  # note a: !logd-patch-root { k0: 7 }
    scope entry@3:  a: !raw { k0: 7 y: 9 }

Note WHERE the comment is in entry@2: at the document root, above `a:`, not
inside a. That is the container/first-field attachment the encoder side of
7c4e04c settled -- a comment written above the first thing in a container
belongs to the container. So entry@3, which states only a, does not contradict
it, and the fold should have kept it.

Reproduced with lowering on. It needs the scope to make a claim -- the same
sequence with `{k9: 1}` in place of the !rename keeps the comment, in both the
lowered and unlowered stores -- so what the fold does with a root comment when
a later entry states a field beneath it is the thing to look at, not the claim
itself.

Found by the claim-stability differential added with the scope-ownership work on
4wpqh7t2h12ks1fvj5n0 (seed 2 there shows the same shape in a longer stream).