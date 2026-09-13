# o view

Render documents, or rewrite them in place with -w.

-w writes each file back in its own format: the one a -j, -y, -t, -I or -O flag
names, else the one its extension names -- .json, .yaml, .yml -- and tony
otherwise. Flags naming two different formats are refused with -w.

A value can travel as a stream of documents, --- separated, or as one document
holding a list, and -split and -gather move between the two:

    o v -split issues.tony     # each element of a list, a document of its own
    o v -gather a.tony b.tony  # every document of every input, one list

-split writes the elements of every document that is a list, and nothing for a
document that is not one, which holds no elements. -gather writes one list, and
[] when there are no documents at all: gathering nothing is a list of nothing.

With them, a command that reads a stream works on a list, and back:

    o v -split issues.tony | o m '{state: open}' | o m '{kind: bug}' | o v -gather

which is "o m -each '!and [{state: open}, {kind: bug}]' issues.tony" whenever an
element matched; when none did, -each writes nothing and answers 1, as a filter
does, where -gather writes [].

Also known as `v`.

## Usage

```
o view [opts] [file...]
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

### `o view` options

| option | type | default | description |
| --- | --- | --- | --- |
| `-c` | bool |  | include comments |
| `-w` | bool |  | write the normalized form back to each file; keeps comments, as -c does |
| `-split` | bool |  | write each element of every list document as a document of its own |
| `-gather` | bool |  | write every document of every input as one list |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [`o`](README.md)
- [`o eval`](o-eval.md)
- [`o diff`](o-diff.md)
- [`o get`](o-get.md)
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

