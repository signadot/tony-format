# logd: a misplaced or misspelled request field is ignored, and a patch with no path writes to the root

A request field in the wrong place, or misspelled, is ignored rather than refused. The
request then means something else, and the server answers as though that is what it was
asked. On a write, the something else is the ROOT.

MEASURED, against a bare logd with `a.b` holding `{k: v}`:

    {patch: {pth: "c.d", data: {k: misplaced}}}   -> committed, and the value landed at
                                                     the ROOT.  Final state:
                                                     {a: {b: {k: v}} k: misplaced}

    {match: {path: "a.b"}}                        -> read the ROOT, answered ok
                                                     (path belongs under body)
    {match: {body: {pth: "a.b"}}}                 -> read the ROOT, answered ok
    {match: {body: {path: "a.b"}, bogus: 1}}      -> ignored the unknown field, read
                                                     "a.b" correctly
    {mach: {body: {path: "a.b"}}}                 -> "no operation specified"

So the OPERATION is checked -- a misspelled op is refused -- and nothing below it is.
A misspelled or misplaced `path` does not fail: it defaults to "", which is the whole
document for a read and the document ROOT for a write.

WHY IT MATTERS MOST ON A WRITE. A read that answers too much is slow and confusing. A
write whose path silently becomes "" merges the client's data into the top of the
document, where nothing it owns lives, and reports a commit and a revision. The client
has no way to tell that from success -- it asked for `c.d`, it was told "committed".
Nothing later reads it back at `c.d`, so the wrong data sits at the root until someone
looks.

It also cost a diagnosis: my own probes in dp5y7ahhh12ksgbvgdn0 used
`{match: {path: ...}}`, were silently answered from the root, and I reported "a read
answers the whole document at any path" as a property of the store. It is not; it was
this.

WHAT THE FIX IS, roughly. FromTonyIR ignores fields it does not know. Refusing them --
an unknown field in a request is an error naming the field, the way an unknown
operation already is -- would turn every case above into a message a client can act on.
The question of degree is whether an unknown field anywhere in a request is refused, or
only in the request envelope: a document a client sends as DATA legitimately holds
anything, so the refusal has to stop at the boundary between the protocol's own fields
and the payload.

A lighter variant, if strictness is too blunt: require `path` where the protocol needs
one rather than defaulting it to the root, so a patch with no path is an error instead
of a write to the top of the document. That fixes the dangerous half without touching
the decoder.