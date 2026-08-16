# o system docd serve

run the [docd](https://signadot.github.io/tony-format/docd/) document server

## Usage

```
o system docd serve [-addr <addr>] [-mount-addr <addr>] [-logd <addr>] [-admin-addr <addr>]
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

### `o system docd serve` options

| option | type | default | description |
| --- | --- | --- | --- |
| `-config` | string |  | configuration file (tony format) |
| `-addr` | string | `localhost:9124` | client-facing TCP listen address ([logd](https://signadot.github.io/tony-format/logd/) session protocol) |
| `-mount-addr` | string | `localhost:9125` | controller-facing (MOUNT) TCP listen address |
| `-logd` | string | `localhost:9123` | [logd](https://signadot.github.io/tony-format/logd/) server address |
| `-admin-addr` | string | `localhost:9224` | admin/pprof listen address, or off to disable |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/o-system-docd-serve/)
- [`o system docd`](o-system-docd.md)
- [`o system docd mounts`](o-system-docd-mounts.md)
- [`o system docd schema`](o-system-docd-schema.md)

