# o list

list or query objects elements from files

The path is a kpath -- .field, [i], {i}, (key), the wildcards .* [*] {*}, and .. for
any depth, which belong here rather than in get: list answers with every node the
path names.

    o list ..image deploy.tony      # every image, wherever it is
    o list 'spec..name' deploy.tony # every name under spec, at any depth

A leading $ is accepted and dropped, from when paths were written that way, and the
$...x spelling of any-depth is read as ..x.

The path says WHERE to look and -if says WHICH of what is there to keep, which
together are how a list is filtered by a match:

    x | o list -if '{state: open}' '$[*]'            # the matching elements
    o list -if '{state: open}' '$.items[*]' doc.tony # at any depth the path reaches

-trim writes only the parts its own match document names, so which nodes and how
much of each are asked separately:

    x | o list -if '{status: running}' -trim '{runner: null, started: null}' '[*]'

A file is optional: with none, stdin is read, as grep and cat do -- from a pipe,
or typed at a terminal and ended with Ctrl-D. An input is a STREAM of documents,
--- separated, and the path is asked of every one of them; the answer is a
single list over all of them, whichever input each node came from.

Without -if the path is the whole question and every node it names is written.
The answer is a list, and the empty list is written as one; the exit code says
whether it was empty: 0 when something was kept, 1 when nothing was, 2 for a
fault.

The match documents -if and -trim take are the ones o match takes; the operators
they may use are at <https://signadot.github.io/tony-format/matchpatch/>

Also known as `l`.

## Usage

```
o list [opts] <kpath> [files]
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

### `o list` options

| option | type | default | description |
| --- | --- | --- | --- |
| `-c` | bool |  | include comments |
| `-if` | string |  | keep only the nodes matching this match document |
| `-if-file` | string |  | read the match document from a file |
| `-trim` | string |  | write only the parts this match document names |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/o-list/)
- [`o`](README.md)
- [`o view`](o-view.md)
- [`o eval`](o-eval.md)
- [`o diff`](o-diff.md)
- [`o get`](o-get.md)
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

