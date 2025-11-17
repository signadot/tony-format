# Matching and Patching

Our starting point is [rfc 7396](https://datatracker.ietf.org/doc/html/rfc7396),
which proposes a standard for using JSON documents as matching criteria of JSON
documents and as declarative criteria used to patch JSON documents.

Tony uses this as a basis for matching and patching object notation which can
be represented as JSON and extends it in various capacities.

## MergeOps

Tony operations are available in matches and patches when building from
directories and via `o match` and `o patch`.


| MergeOp    | Match | Patch | Arguments         | Description                                                                     |
|------------|-------|-------|-------------------|---------------------------------------------------------------------------------|
| key        |   +   |   +   | objectpath to key | associative lists as objects                                                    |
| and        |   +   |   -   |     -             | conjoin a list of matches to be applied to the corresponding doc                |
| or         |   +   |   -   |     -             | disjunction                                                                     |
| not        |   +   |   -   |     -             | negate a match (eg !not.or [1,2,3])
| all        |   +   |   +   |     -             | take the match (resp patch) apply it to all array or object elements of the doc |
| subtree    |   +   |   -   |     -             | match any subtree of the doc                                                    |
| dive       |   -   |   +   |     -             | dive into the doc and treat each subtree with a list of matches/patches         |
| quote      |   -   |   +   |     -             | quote a yaml as a string                                                        |
| unquote    |   -   |   +   |     -             | unquote a string as a yaml                                                      |
| nullify    |   -   |   +   |     -             | turn a yaml into a null without deleting it                                     |
| delete     |   -   |   +   |     -             | delete a top level document                                                     |
| type       |   +   |   -   |     -             | match by type                                                                   |
| field      |   +   |   +   |     -             | match the field (a string), not its value                                       |
| tag        |   +   |   -   |     -             | match the tag of a node, not its value                                          |
| glob       |   +   |   -   |     -             | glob match a string                                                             |
| pipe       |   -   |   +   |     -             | pipe the doc node to a program and replace it with the program's output         |
| json-patch | -     |   +   |     -             | apply a json patch to the corresponding doc node                                |
| pass       |   +   |   +   |     -             | match: always accept / patch: return the current doc                            |
| if         |   -   |   +   |     -             | evaluate a condition and patch either with `then` or `else`                     |

Operations are indicated by YAML tags within a match or a patch.

Most operations are either match operations or patch operations but not both.
Some operations, such as `key` and `field`, are both.

### Considerations

Contrary to evaluation tags, match and patch operations relate the match (or
patch) document to some _input_ document.  Evaluation tags just relate the node
in the document in which they reside to the environment.

This relating of match or patch doc leads to some interesting cases.

For example, let's consider the `and` and `all` matches.   The `and` match
consists of a list of matches, each of which must match the corresponding input
document.  The `all` match consists of a single match which must apply to all
array or object members of the document.

As a result `and` is not a patch operation.  However, `all` is both a match and
a patch operation:  as a patch it applies the child patch to all object or
array members of the corresponding input document.

## Custom Ops

Match and patch operations can be created by implementing a simple interface
and registering the operation in the `mergeop` package.
