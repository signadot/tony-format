# o system logd serve

run the [logd](https://signadot.github.io/tony-format/logd/) storage server

## Usage

```
o system logd serve -data <dir> [-addr <addr>] [-admin-addr <addr>]
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

### `o system logd serve` options

| option | type | default | description |
| --- | --- | --- | --- |
| `-data` | string |  | directory for [logd](https://signadot.github.io/tony-format/logd/) data |
| `-config` | string |  | configuration file (tony format) |
| `-addr` | string | `localhost:9123` | TCP listen address |
| `-admin-addr` | string | `localhost:9223` | admin/pprof listen address, or off to disable |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/o-system-logd-serve/)
- [`o system logd`](o-system-logd.md)
- [`o system logd session`](o-system-logd-session.md)

