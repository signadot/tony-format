package main

import (
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/tony-format/tony"
	"github.com/tony-format/tony/encode"
	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/libdiff"
	"github.com/tony-format/tony/parse"

	"github.com/scott-cotton/cli"
)

func diff(cfg *DiffConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Diff.Parse(cc, args)
	if err != nil {
		cfg.Diff.Usage(cc, err)
		return cli.ExitCodeErr(1)
	}
	if cfg.Loop == "" {
		if len(args) != 2 {
			return fmt.Errorf("%w: diff (without -loop) requires 2 args, got %v", cli.ErrUsage, args)
		}
		y1, err := getObjFile(cc, args[0], cfg.parseOpts()...)
		if err != nil {
			return fmt.Errorf("error decoding %s: %w", args[0], err)
		}
		y2, err := getObjFile(cc, args[1], cfg.parseOpts()...)
		if err != nil {
			return fmt.Errorf("error decoding %s: %w", args[1], err)
		}
		diff, err := diffInputs(cfg, cc, y1, y2, false)
		if err != nil {
			return err
		}
		if diff {
			return cli.ExitCodeErr(1)
		}
		return nil
	}

	return diffLoop(cfg, cc)
}

func diffLoop(cfg *DiffConfig, cc *cli.Context) error {
	i := 0
	last := ir.Null()
	ticker := time.NewTicker(cfg.LoopEvery)
	defer ticker.Stop()
	diffCount := 0
	for {
		if i == cfg.LoopLim {
			break
		}
		cmd := exec.Command("sh", "-c", cfg.Loop)
		r, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("unable to create pipe for command %q: %w", cfg.Loop, err)
		}
		cmd.WaitDelay = cfg.LoopEvery
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("unable to start %q: %w", cfg.Loop, err)
		}
		d, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		next, err := parse.Parse(d, cfg.parseOpts()...)
		if err != nil {
			return fmt.Errorf("error decoding command output: %w", err)
		}
		differs, err := diffInputs(cfg, cc, last, next, diffCount > 0)
		if err != nil {
			return err
		}
		if differs {
			diffCount++
		}

		if err != nil {
			return fmt.Errorf("unable to decode next object: %w", err)
		}
		if err := cmd.Wait(); err != nil {
			return fmt.Errorf("command %q exited with an error: %w", cfg.Loop, err)
		}
		last = next
		<-ticker.C
		i++
	}
	return nil
}

func diffInputs(do *DiffConfig, cc *cli.Context, a, b *ir.Node, sep bool) (bool, error) {
	d := tony.Diff(a, b)
	w := cc.Out
	if d == nil {
		return false, nil
	}
	when := time.Now().Format(time.RFC3339Nano)
	if do.Reverse {
		rev, err := libdiff.Reverse(d)
		if err != nil {
			return false, fmt.Errorf("error reversing: %w", err)
		}
		d = rev
	}
	if sep {
		_, err := w.Write([]byte("---\n"))
		if err != nil {
			return false, fmt.Errorf("unable to write separator: %w", err)
		}
	}
	if do.Loop != "" {
		_, err := w.Write([]byte("# difference found at " + when + "\n"))
		if err != nil {
			return false, err
		}
	}
	if err := encode.Encode(d, w, do.MainConfig.encOpts(w)...); err != nil {
		return false, err
	}
	return true, nil
}
