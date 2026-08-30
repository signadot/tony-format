# o completion

bash|zsh|fish

Write a shell completion script.

The script is generated from the command tree in this binary, so it completes the
commands and options this o has, rather than the ones some other copy had.

    o completion zsh  > "${fpath[1]}/_o"          # zsh
    o completion bash > /etc/bash_completion.d/o  # bash
    source <(o completion bash)                   # bash, this shell only
    o completion fish > ~/.config/fish/completions/o.fish

Commands and options are completed; a path into a document is not, since knowing
one means reading a file the command line has not finished naming.

## Usage

```
o completion [options] [arguments]
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

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/o-completion/)
- [`o`](README.md)
- [`o view`](o-view.md)
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
- [`o version`](o-version.md)

