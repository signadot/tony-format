# token: a '- ' whose value is a bracket on the next line is refused as dangling (regression in go-tony/v0.0.247)

The block encoder writes a tagged flow object under an element with its opening bracket at column 0: `[!not {a: 1}]` held as a block array encodes as `- !not\n{\n  a: 1\n}\n`. Since go-tony/v0.0.247 the parser refuses this: "a '- ' must be followed by a value". `- !not\n[\n  1\n]\n` is refused the same way. Both parsed through v0.0.246. verse found it.

The cause is 3618245f (v5dn9vfbh12krj9xwsn0). To refuse `- !t` with nothing after it, it calls `danglingElt` after the tag. `danglingElt` treats a next line at or left of the dash's column as "no value", but a `{` or `[` is a value whatever its column. A field already reads it so: `x: !not\n{…}` and `x:\n{…}` parse.

The untagged shape `- \n{…}` has been refused since before v0.0.240, while `x:\n{…}` parses. It is the same rule enforced on a field but not on an element, so it is fixed in the same pass.

The fix: `danglingElt` does not call a '- ' dangling when what follows the line break, past any comment lines, is an opening `{` or `[`.

The wire decoder reads bracketed form only, so it has no `- ` shape to agree on.