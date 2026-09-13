package main

import (
	"context"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/system/lsp"
)

const lspDesc = `Serve the Tony language server to an editor.

An editor starts the command and speaks the Language Server Protocol to it on
standard input and output. It reports parse errors as diagnostics and answers
hover, formatting, completion and semantic-token requests, using the same parser
and encoder as the rest of o. The handshake names the server "o system lsp" and
reports which build it is, so an editor's server log shows what its
configuration resolved to; ` + "`o version`" + ` says the same at a shell.

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
didClose, hover, formatting, completion and semanticTokens.`

type LSPConfig struct {
	*MainConfig
	LSP *cli.Command
}

func LSPCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &LSPConfig{MainConfig: mainCfg}
	return cli.NewCommandAt(&cfg.LSP, "lsp").
		WithSynopsis("lsp [opts]").
		WithDescription(lspDesc).
		WithRun(func(cc *cli.Context, args []string) error {
			return runLSP(cfg, cc, args)
		})
}

func runLSP(cfg *LSPConfig, cc *cli.Context, args []string) error {
	args, err := cfg.LSP.Parse(cc, args)
	if err != nil {
		return err
	}
	if helpAsked(cfg.LSP, cc, cfg.Help) {
		return nil
	}
	if len(args) != 0 {
		return usageErr(cfg.LSP, cc, "lsp takes no arguments: an editor speaks the protocol on stdin and stdout")
	}
	return lsp.Serve(context.Background(), cc.In, cc.Out)
}
