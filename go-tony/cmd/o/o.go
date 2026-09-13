package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/scott-cotton/cli"
)

func oMain(cfg *MainConfig, cc *cli.Context, args []string) error {
	defer func() {
		if cfg.CloseOut != nil {
			cfg.CloseOut()
		}
	}()
	cfg.argv = args
	args, err := cfg.Main.Parse(cc, args)
	if err != nil {
		cfg.Main.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	if err := cfg.oneFormat(cfg.Main, cc); err != nil {
		return err
	}
	// Asking what a tool does is not a misuse of it. `o -h`, `o --help` and `o` with
	// nothing at all all print the same thing, on stdout, and exit 0 -- they used to
	// print it on stderr followed by `usage error: unknown option: "help"`, which
	// tells a first-time reader they have already got it wrong.
	if cfg.Help || len(args) == 0 {
		if cfg.Help && len(args) > 0 {
			if sub := cfg.Main.FindSub(cc, args[0]); sub != nil {
				sub.Usage(cc, nil)
				return nil
			}
		}
		cfg.Main.Usage(cc, nil)
		if !cfg.Help {
			fmt.Fprintf(cc.Out, "\nplease choose a command, or run `o help <command>`.\n")
		}
		return nil
	}
	sub := cfg.Main.FindSub(cc, args[0])
	if sub == nil {
		return noSuchCommand(cfg.Main, cc, args[0])
	}
	err = sub.Run(cc, args[1:])
	if errors.Is(err, cli.ErrUsage) {
		// The subcommand's usage is what a misuse of the subcommand calls for, not
		// the root's -- which is what the library would print if this error went
		// back as it is. So it is written here and an exit code goes back in its
		// place, since an ExitCodeErr is neither printed again nor mistaken for a
		// usage error at the top.
		//
		// It is returned rather than exited on: os.Exit here skipped the deferred
		// CloseOut above, so `o -o out.tony <cmd>` with a misuse in <cmd> left
		// out.tony unflushed and unclosed.
		sub.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	return err
}

// oneFormat refuses more than one of -t, -j and -y.
//
// They name what a document IS, and a document is one thing, so two of them is not a
// preference to resolve but a question the caller has to answer. Resolving it silently is
// what happened before: the switch in parseOpts and encOpts tries tony first, so `-j -t`
// read json as tony and said nothing.
//
// It is asked per COMMAND as well as at the root, because these are the root's options
// and every command accepts them in its own position too -- `o -j -t v f` was refused
// while `o v -j -t f` was not, which is the same mistake spelled differently.
func (cfg *MainConfig) oneFormat(cmd *cli.Command, cc *cli.Context) error {
	if count(cfg.T, cfg.J, cfg.Y) > 1 {
		return usageErr(cmd, cc, "must specify at most one of -j[son] -t[ony] -y[aml]")
	}
	return nil
}

func count(vs ...bool) int {
	ttl := 0
	for _, v := range vs {
		if v {
			ttl++
		}
	}
	return ttl
}

func (cfg *MainConfig) outOpt(cc *cli.Context, a string) (any, error) {
	cfg.Out = a
	if a == "-" {
		return nil, nil
	}
	if in := cfg.inputNamedByOut(a); in != "" {
		return nil, fmt.Errorf("%w: -o %q is also the input %q; the output is opened, and emptied, before anything is read (view -w rewrites a file in place)", cli.ErrUsage, a, in)
	}
	f, err := os.OpenFile(cfg.Out, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	cc.Out = f
	cfg.CloseOut = f.Close
	return nil, nil
}

// inputNamedByOut is the argument, if any, that names the same file as -o does,
// or "". The output is opened with O_TRUNC while the options are being parsed,
// before the command reads anything, so `o -o f v f` read an empty f and exited 0
// with f empty: a misuse, but a silent one, and the shell's `> f` is no help
// since o cannot tell it from any other stdout. The argument is the -o value
// itself when it follows -o or --o (the -o=f spelling is one argument, and skipped
// as an option); every other argument that is the same file is an input.
func (cfg *MainConfig) inputNamedByOut(out string) string {
	st, err := os.Stat(out)
	if err != nil {
		return "" // nothing there to empty
	}
	for i, arg := range cfg.argv {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if i > 0 && (cfg.argv[i-1] == "-o" || cfg.argv[i-1] == "--o") {
			continue
		}
		if ast, err := os.Stat(arg); err == nil && os.SameFile(st, ast) {
			return arg
		}
	}
	return ""
}
