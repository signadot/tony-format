# o get

Read a single node at a path, from files or standard input.

The path is a kpath, the syntax the rest of the system uses: .field steps into an
object, [i] into an array, {i} into a sparse one by key, and (key) into a keyed
array by identity, so "o get 'items(WIDGET).qty'" names an element wherever it
sits. A
leading $ is accepted and dropped, from when paths were written that way.

get answers with a single node, so it refuses the paths which may name many: a
wildcard, or .. for any depth. Those belong to list.

The whole document is "." or the empty path. Giving no path is a usage error, since
a missing path and a path naming everything are different mistakes.

The path says where to look. -if says which of what is there to keep: the node
is written only when it matches the match document given, so

    o get -if '{state: open}' '$.items[0]' doc.tony && deploy

reads as the guard it is. -trim writes only the parts its own match document
names. A file is optional: with none, stdin is read.

An input is a stream of documents, --- separated, and the path is asked of each
one -- which is what makes the output of one command the input of the next:

    o get .spec a.tony b.tony | o get .replicas

A comment describes a value and is not the value itself, so -if sees through
one. !comment is how it asks about the comments instead, and it needs -c --
without it the comments are never read and there is nothing to ask about:

    o get -c -if '!comment {head: ["# generated"]}' . doc.tony && regen

A head comment belongs to the value written under it and a line comment to the
value it follows, so the question is asked at that node and not at the one above:

    o get -c -if '{name: !comment {line: [" # keep"]}}' 'items[1]' doc.tony

Exit codes are the search convention: 0 when something was written, 1 when the
path named nothing or what it named did not match, 2 for a fault.

The match documents -if and -trim take are the ones o match takes; the operators
they may use are at <https://signadot.github.io/tony-format/matchpatch/>

Also known as `g`, `ge`.

## Usage

```
o get [opts] <kpath> [file...]
```

## Options

### inherited from `o`

| option | type | default | description |
| --- | --- | --- | --- |
| `-b` | bool |  | encode with brackets |
| `-x` | bool |  | expand <<: merge field while encoding |
| `-color` | bool |  | colorize; on by default to a terminal, -color=false to suppress |
| `-wire` | bool |  | output in compact format |
| `-h`, `-help` | bool |  | show help for this command |
| `-t`, `-tony` | bool |  | do i/o in tony |
| `-j`, `-json` | bool |  | do i/o in json |
| `-y`, `-yaml` | bool |  | do i/o in yaml |
| `-o` | (filepath) |  | output file (default stdout) |
| `-I`, `-ifmt` | (format) |  | input format: tony/t, json/j, yaml/y |
| `-O`, `-ofmt` | (format) |  | output format: tony/t, json/j, yaml/y |

### `o get` options

| option | type | default | description |
| --- | --- | --- | --- |
| `-c` | bool |  | include comments, and let a !comment -if pattern see them |
| `-if` | string |  | keep the node only if it matches this match document |
| `-if-file` | string |  | read the match document from a file |
| `-trim` | string |  | write only the parts this match document names |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/o-get/)
- [`o`](README.md)
- [`o view`](o-view.md)
- [`o eval`](o-eval.md)
- [`o diff`](o-diff.md)
- [`o list`](o-list.md)
- [`o match`](o-match.md)
- [`o patch`](o-patch.md)
- [`o build`](o-build.md)
- [`o dump`](o-dump.md)
- [`o load`](o-load.md)
- [`o schema`](o-schema.md)
- [`o system`](o-system.md)
- [`o docs`](o-docs.md)
- [`o help`](o-help.md)
- [`o completion`](o-completion.md)
- [`o version`](o-version.md)

