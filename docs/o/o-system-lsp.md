# o system lsp

Serve the Tony language server to an editor.

An editor starts the command and speaks the Language Server Protocol to it on
standard input and output. It reports parse errors as diagnostics and answers
hover, formatting, completion and semantic-token requests, using the same parser
and encoder as the rest of o. The handshake names the server "o system lsp" and
reports which build it is, so an editor's server log shows what its
configuration resolved to; `o version` says the same at a shell.

Point the editor's LSP client at the command. Neovim, with nvim-lspconfig:

    require('lspconfig').tony.setup({
      cmd = {'o', 'system', 'lsp'},
      filetypes = {'tony'},
      root_dir = function(fname) return vim.fn.getcwd() end,
    })

Vim, with vim-lsp:

    if executable('o')
      au User lsp_setup call lsp#register_server({
        \ 'name': 'tony',
        \ 'cmd': {server_info->['o', 'system', 'lsp']},
        \ 'whitelist': ['tony'],
        \ })
    endif

VS Code, through a generic LSP client extension: command "o", arguments
["system", "lsp"], for the tony language.

What the server answers: textDocument/didOpen, didChange (incremental sync),
didClose, hover, formatting, completion and semanticTokens.

## Usage

```
o system lsp [opts]
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

- [`o system`](o-system.md)
- [`o system logd`](o-system-logd.md)
- [`o system docd`](o-system-docd.md)
- [`o system session`](o-system-session.md)
- [`o system up`](o-system-up.md)

