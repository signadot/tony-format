package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"

	"github.com/scott-cotton/cli"
)

func list(cfg *ListConfig, cc *cli.Context, args []string) error {
	args, err := cfg.List.Parse(cc, args)
	if err != nil {
		cfg.List.Usage(cc, err)
		return cli.ExitCodeErr(1)
	}
	if len(args) == 0 {
		return fmt.Errorf("%w: list requires one argument, an object path", cli.ErrUsage)
	}
	path := args[0]
	if path == "" {
		return fmt.Errorf("%w: invalid query \"\"", cli.ErrUsage)
	}
	if path[0] != '$' {
		path = "$" + path
	}
	args = args[1:]
	for _, arg := range args {
		if err := queryArg(cfg.MainConfig, cc.Out, arg, path, true, false); err != nil {
			return fmt.Errorf("error querying %s with %s: %w", arg, path, err)
		}
	}
	return nil
}

func queryArg(cfg *MainConfig, w io.Writer, arg, query string, list, sep bool) error {
	var targetReader io.Reader
	if arg == "-" {
		targetReader = os.Stdin
	} else {
		targetFile, err := os.Open(arg)
		if err != nil {
			return fmt.Errorf("error opening %s: %w", arg, err)
		}
		defer targetFile.Close()
		targetReader = targetFile
	}
	rd, err := io.ReadAll(targetReader)
	if err != nil {
		return err
	}
	target, err := parse.Parse(rd, cfg.parseOpts()...)
	if err != nil {
		return fmt.Errorf("error decoding %s: %w", arg, err)
	}
	if list {
		res, err := target.ListPath(nil, query)
		if err != nil {
			return fmt.Errorf("error executing list on %s: %w", arg, err)
		}
		arr := ir.FromSlice(res)
		if err := encode.Encode(arr, w, cfg.encOpts(w)...); err != nil {
			return fmt.Errorf("error encoding result: %w", err)
		}
		return nil
	}
	res, err := target.GetPath(query)
	if err != nil {
		return fmt.Errorf("error executing get on %s: %w", arg, err)
	}
	if res == nil {
		// don't encode anything and don't yell either
		return nil
	}
	if sep {
		if err := writeSep(w); err != nil {
			return err
		}
		argLines := strings.Split(strings.TrimSpace(arg), "\n")
		for i, argLine := range argLines {
			msg := "# from " + argLine + "\n"
			if i != 0 {
				msg = "#     " + argLine + "\n"
			}
			_, err := w.Write([]byte(msg))
			if err != nil {
				return err
			}
		}

	}
	if err := encode.Encode(res, w, cfg.encOpts(w)...); err != nil {
		return fmt.Errorf("error encoding result: %w", err)
	}
	return nil

}
