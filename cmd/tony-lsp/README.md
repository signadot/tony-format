# Tony Language Server

A Language Server Protocol (LSP) implementation for the Tony format, providing syntax validation, hover information, formatting, and code completion.

## Features

- **Syntax Validation**: Real-time diagnostics for Tony format syntax errors
- **Hover Information**: Type and value information when hovering over elements
- **Document Formatting**: Format Tony documents according to the Tony specification
- **Code Completion**: Context-aware completion suggestions for tags, keywords, and constructs

## Building

```bash
go build ./cmd/tony-lsp
```

## Usage

The language server communicates via stdio using the Language Server Protocol. It can be used with any LSP-compatible editor.

### VS Code

Create a VS Code extension configuration or use a generic LSP client:

```json
{
  "name": "tony-lsp",
  "command": "tony-lsp",
  "args": []
}
```

### Neovim

Using nvim-lspconfig:

```lua
require('lspconfig').tony_lsp.setup({
  cmd = {'tony-lsp'},
  filetypes = {'tony'},
  root_dir = function(fname)
    return vim.fn.getcwd()
  end,
})
```

### Vim

Using vim-lsp:

```vim
if executable('tony-lsp')
  au User lsp_setup call lsp#register_server({
    \ 'name': 'tony-lsp',
    \ 'cmd': {server_info->['tony-lsp']},
    \ 'whitelist': ['tony'],
    \ })
endif
```

## Supported LSP Features

- `textDocument/didOpen` - Document opened
- `textDocument/didChange` - Document changed (incremental sync)
- `textDocument/didClose` - Document closed
- `textDocument/hover` - Hover information
- `textDocument/formatting` - Document formatting
- `textDocument/completion` - Code completion

## Implementation Details

The language server uses the existing Tony parser (`ytool/parse`) and encoder (`ytool/encode`) to provide language features. It maintains an in-memory document store and publishes diagnostics as documents are edited.
