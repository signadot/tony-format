# o diff

a b or diff -loop <cmd>

diff object documents

Given two arguments, diff writes what turns the first into the second, and
exits 1 if they differ at all.

Loop Mode

With -loop <cmd>, diff runs the command every -loopEvery and writes what
changed since the run before, until -loopLim runs have happened.  The command
is expected to write an object; watching one is what this is for.

-loopUntil <match> stops the loop once the command's output matches, which
makes diff a way to wait for a state and see how it got there:

    o d -loop 'kubectl get pod x -o yaml' -loopUntil '{status: {phase: Running}}'

The match is a match object, as taken by 'o match', so it need only name the
fields it cares about.  It is matched against what the command wrote and not
against the difference, and it is checked after that difference is written, so
the change which satisfied it is the last thing reported.  Should the loop hit
-loopLim first, diff exits 1: the condition asked for did not hold.

Also known as `d`, `di`.

## Usage

```
o diff [options] [arguments]
```

## Options

### inherited from `o`

| option | type | default | description |
| --- | --- | --- | --- |
| `-b` | bool |  | encode with brackets |
| `-x` | bool |  | expand <<: merge field while encoding |
| `-color` | bool |  | encode with color |
| `-wire` | bool |  | output in compact format |
| `-t`, `-tony` | bool |  | do i/o in tony |
| `-j`, `-json` | bool |  | do i/o in json |
| `-y`, `-yaml` | bool |  | do i/o in yaml |
| `-o` | (filepath) |  | output file (default stdout) |
| `-I`, `-ifmt` | (format) |  | input format: tony/t, json/j, yaml/y |
| `-O`, `-ofmt` | (format) |  | output format: tony/t, json/j, yaml/y |

### `o diff` options

| option | type | default | description |
| --- | --- | --- | --- |
| `-c` | bool |  | include comments, and report differences in them |
| `-r` | bool |  | reverse the diff |
| `-loop` | string |  | command to produce objects to diff in a loop |
| `-loopLim` | int |  | max number of times to loop |
| `-loopUntil` | string |  | stop once the looped command output matches this match object |
| `-loopEvery` | (func) |  | how long to wait between runs of the looped command |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/o-diff/)
- [`o`](README.md)
- [`o view`](o-view.md)
- [`o eval`](o-eval.md)
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
- [`o version`](o-version.md)

