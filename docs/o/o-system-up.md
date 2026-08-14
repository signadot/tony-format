# o system up

start [logd](https://signadot.github.io/tony-format/logd/) and [docd](https://signadot.github.io/tony-format/docd/) servers

## Usage

```
o system up -data <dir> [-config <file>]
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

### `o system up` options

| option | type | default | description |
| --- | --- | --- | --- |
| `-data` | string |  | data directory for [logd](https://signadot.github.io/tony-format/logd/) storage |
| `-config` | string |  | [logd](https://signadot.github.io/tony-format/logd/) configuration file (tony format), as for o system logd serve |
| `-logd-addr` | string | `localhost:9123` | [logd](https://signadot.github.io/tony-format/logd/) listen address |
| `-docd-addr` | string | `localhost:9124` | [docd](https://signadot.github.io/tony-format/docd/) client-facing listen address |
| `-docd-mount-addr` | string | `localhost:9125` | [docd](https://signadot.github.io/tony-format/docd/) controller-facing (MOUNT) listen address |
| `-admin-addr` | string | `localhost:9223` | admin/pprof listen address, or off to disable |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/o-system-up/)
- [`o system`](o-system.md)
- [`o system logd`](o-system-logd.md)
- [`o system docd`](o-system-docd.md)

