# Operation Reference

This documentation is automatically generated from the operation registry.

## Eval Operations

See [eval operations](./eval.md) for details.

| Operation | Summary |
|-----------|--------|
| `!os_env` | Get value from OS environment variable |
| `!to_string` | Convert a value to a string |
| `!to_int` | Convert a value to an integer |
| `!eval` | Evaluate environment variables in a document |
| `!file` | Load content from a file or URL |
| `!exec` | Execute a shell command and capture output |

## Mergeop Operations

Every operation the registry holds, with a one-line summary.
`mergeop.TestIndexTableCoversEveryOperation` fails when one is added without a row here.
[mergeop operations](./mergeop.md) has an entry per operation with examples;
[matchpatch.md](../matchpatch.md) has the same list with which of match and patch each is
for.

`!key` is here too, though it is not an operation in the sense the others are: it declares
that an array is keyed by a field, so its elements merge by identity rather than by
position. It is used alongside these constantly, and a reader who does not find it concludes
it does not exist — see [Keyed arrays](../logd/keyed.md).

| Operation | Summary |
|-----------|--------|
| `!addtag` | add a tag; the tag is what results |
| `!all` | apply the match (resp. patch) to every element of an array or object |
| `!and` | conjoin a list of matches, each applied to the corresponding doc |
| `!arraydiff` | an array edit, relative and positional |
| `!at` | walk to the path and apply the match there; see below |
| `!comment` | match or state the comments here; the operand names head, line or both, `[]` for none |
| `!delete` | remove a value; absence is what results |
| `!dive` | dive into the doc and treat each subtree with a list of matches/patches |
| `!embed` | the operand is the result, with each occurrence of the key replaced by the doc |
| `!field` | match the field (a string), not its value |
| `!get-path` | the node at a kpath: `!get-path(root) spec.image` |
| `!glob` | glob match a string |
| `!has-path` | the document has the path the operand names |
| `!if` | evaluate `if:` and patch with `then:` or `else:` |
| `!insert` | add a value; the value is what results |
| `!ir` | match the node's IR fields, not its value: `!ir {int: .[number]}` an integer |
| `!irtype` | the node's kind equals the operand's: `!irtype ""` a string, `!irtype 0` a number |
| `!json-patch` | apply a json patch to the corresponding doc node |
| `!key` | associative lists as objects |
| `!let` | bind names in `let:`, then match or patch with `in:`, referring to them as `.[name]` |
| `!list-path` | the nodes at a kpath as a list; takes the wild paths `!get-path` refuses |
| `!not` | negate a match (eg `!not.or [1,2,3]`) |
| `!nullify` | turn a node into a null without deleting it |
| `!or` | disjunction |
| `!pass` | match: accept anything / patch: leave the document as it is |
| `!pipe` | pipe the doc node to a program and replace it with the program's output |
| `!quote` | quote a document as a string |
| `!raw` | the escape: treat the subtree as data, interpreting no operation at any depth |
| `!rename` | rename fields, relative to the keys that are there |
| `!replace` | CHECKED: verify the node still equals `from:`, then install `to:` |
| `!retag` | CHECKED: verify the tag is `from`, then make it `to` |
| `!rmtag` | remove a tag; its absence is what results |
| `!strdiff` | a string edit, relative to the string that is there |
| `!subtree` | match any subtree of the doc |
| `!tag` | match the tag of a node, not its value |
| `!unquote` | unquote a string as a document |
