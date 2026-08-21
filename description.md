# encode: a merge key comes back quoted, and a quoted merge key is not a merge key

A merge key does not survive an encode round trip:

    parse   a: 1
            <<: "{{ tpl }}"
            b: 2                     field 1 is Null-typed -- a merge key

    encode  a: 1
            "<<": "{{ tpl }}"
            b: 2

    parse   that                     field 1 is String-typed, "<<"

What comes back is a different document. `<<` is a token in the grammar, and once it is
quoted it is an ordinary field whose name happens to be two angle brackets -- nothing
downstream can tell it was ever a merge key. The IR is right on both sides of the encode;
it is the spelling that loses.

WHERE. encodeMergeField (encode/encode.go:568) writes the key as

    writeField(w, ir.MergeKey, es)

and writeField (encode/encode.go:1119) quotes whatever it is handed when token.NeedsQuote
says so. token.NeedsQuote("<<") is true, correctly -- `<<` is not a name a field may
have. The merge key is not a field name, so it is the wrong question to ask about it.
writeField already knows that eight lines further down, where it colours `f ==
ir.MergeKey` differently from every other field.

These are the only two call sites: line 560 hands it a real field name, line 574 hands it
this constant. So nothing else is relying on the quoting of something that is not a name.

A bare `<<:` parses back to a null-typed field, so emitting it unquoted round trips.

The injectRaw encoding option (docs/tony.md, "String merge keys") takes the other branch
of encodeMergeField and writes the value at the mapping's indentation with no key at all,
so it is the DEFAULT path that is wrong.

WHY IT IS FILED RATHER THAN FIXED. It turned up while fixing !rename, which was deleting
merge keys outright by routing the object through ir.ToMap -- a different bug, in mergeop,
fixed in 69b3d2f. This one is in encode, it is the format's own spelling of its own token,
and it was not in the scope I was asked for.

One test pins the current spelling: go-tony/rename_test.go, "a merge key is left where it
is", asserts `"<<"` with a comment saying the quoting is the encoder's to answer for and
pointing here. Fixing this flips that one expectation, on purpose. Nothing else in the
tree asserts either spelling.

What the fix should probably also come with is a round-trip test for the merge key itself,
which is what would have caught this: there is none today.