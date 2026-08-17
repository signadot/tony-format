# o schema check

validate documents against a [schema](https://signadot.github.io/tony-format/tonyschema/)

Every document of every file is checked, and every file is checked even after one
of them fails, so one run says how much is wrong rather than just that something
is.

With no files, stdin is read, as grep and cat do.

Exit codes are the tool's usual three: 0 when everything checked satisfies the
schema, 1 when something did not -- which is an answer about the document -- and 2
for a fault: a schema that cannot be read, a file that cannot be opened, or input
which is not object notation at all.

## Usage

```
o schema check <schema-file> [doc-files...]
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

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/o-schema-check/)
- [`o schema`](o-schema.md)

