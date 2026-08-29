# o patch

patch object documents

The patch is applied to every document of every input and each result is written,
--- separated.

Files are optional: with none, stdin is read, as grep and cat do. An input is a
STREAM of documents, so a pipeline is written the obvious way:

    o get .spec a.tony b.tony | o patch '{replicas: 3}'

Without -c the result carries no comments -- not the patch's, and not the ones
the document being patched already had -- because a patch answers with data.

!comment states what the comments at a node ARE, which is how a comment is
changed without rewriting the value it describes. It needs -c as well, or the
comment it states is dropped from what is written:

    o p -c '{replicas: !comment {head: ["# bumped for the launch"]}}' d.tony
    o p -c '!comment {head: []}' d.tony   # drop what is written above the doc

A patch which deletes a whole document writes nothing for it, which is the result
and not a fault. Exit codes: 0, and 2 for a fault.

o patch -tags lists the operators a patch may use, from this binary. What each one
means is at <https://signadot.github.io/tony-format/matchpatch/>

Also known as `p`, `pa`.

## Usage

```
o patch [opts] <patchobj> [files]
```

## Options

### inherited from `o`

| option | type | default | description |
| --- | --- | --- | --- |
| `-b` | bool |  | encode with brackets |
| `-x` | bool |  | expand <<: merge field while encoding |
| `-color` | bool |  | encode with color |
| `-wire` | bool |  | output in compact format |
| `-h`, `-help` | bool |  | show help for this command |
| `-t`, `-tony` | bool |  | do i/o in tony |
| `-j`, `-json` | bool |  | do i/o in json |
| `-y`, `-yaml` | bool |  | do i/o in yaml |
| `-o` | (filepath) |  | output file (default stdout) |
| `-I`, `-ifmt` | (format) |  | input format: tony/t, json/j, yaml/y |
| `-O`, `-ofmt` | (format) |  | output format: tony/t, json/j, yaml/y |

### `o patch` options

| option | type | default | description |
| --- | --- | --- | --- |
| `-c` | bool |  | include comments, of the document as well as the patch |
| `-r` | bool |  | apply diff reversed |
| `-s` | bool |  | patch arg as string |
| `-f` | bool |  | patch arg as file |
| `-tags` | bool |  | show available tags |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/o-patch/)
- [`o`](README.md)
- [`o view`](o-view.md)
- [`o eval`](o-eval.md)
- [`o diff`](o-diff.md)
- [`o get`](o-get.md)
- [`o list`](o-list.md)
- [`o match`](o-match.md)
- [`o build`](o-build.md)
- [`o dump`](o-dump.md)
- [`o load`](o-load.md)
- [`o schema`](o-schema.md)
- [`o system`](o-system.md)
- [`o docs`](o-docs.md)
- [`o help`](o-help.md)
- [`o completion`](o-completion.md)
- [`o version`](o-version.md)

