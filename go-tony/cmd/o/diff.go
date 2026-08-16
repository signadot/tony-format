package main

import (
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/parse"

	"github.com/scott-cotton/cli"
)

func diff(cfg *DiffConfig, cc *cli.Context, args []string) error {
	// diff's exit codes are diff(1)'s, and 1 is a RESULT -- the documents differ --
	// so a fault must not share it. It did: a file that could not be read exited 1,
	// which is exactly what "they differ" says, and a script comparing a path that
	// had moved read it as a difference.
	args, err := cfg.Diff.Parse(cc, args)
	if err != nil {
		cfg.Diff.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	if helpAsked(cfg.Diff, cc, cfg.Help) {
		return nil
	}
	if cfg.LoopUntil != "" && cfg.Loop == "" {
		return usageErr(cfg.Diff, cc, "-loopUntil is a condition on -loop, which was not given")
	}
	if cfg.Loop == "" {
		// One operand means the other is standard input, as a missing file does
		// everywhere else: `o diff baseline.tony` reads as "what turns the baseline
		// into what I piped in", which is the order diff writes anyway. Naming it
		// with - still works and is what to write when stdin is the FIRST operand.
		//
		// Two of them cannot be left out: both sides would be the same stream, and
		// a document does not differ from itself.
		switch len(args) {
		case 1:
			args = append(args, "-")
		case 2:
		default:
			return usageErr(cfg.Diff, cc,
				fmt.Sprintf("diff (without -loop) compares 2 documents, and reads one of them from "+
					"standard input when given only 1; got %v", args))
		}
		y1, err := getObjFile(cc, args[0], cfg.parseOpts()...)
		if err != nil {
			return fault(cc, fmt.Errorf("error decoding %s: %w", args[0], err))
		}
		y2, err := getObjFile(cc, args[1], cfg.parseOpts()...)
		if err != nil {
			return fault(cc, fmt.Errorf("error decoding %s: %w", args[1], err))
		}
		diff, err := diffInputs(cfg, cc, y1, y2, false)
		if err != nil {
			return fault(cc, err)
		}
		if diff {
			return cli.ExitCodeErr(1)
		}
		return nil
	}

	return diffLoop(cfg, cc)
}

func diffLoop(cfg *DiffConfig, cc *cli.Context) error {
	// The condition is decoded before the first command runs.  A match object
	// which does not parse is a usage error, and finding that out on whichever
	// iteration would otherwise have satisfied it is finding it out too late.
	var until *ir.Node
	if cfg.LoopUntil != "" {
		var err error
		until, err = getish(false, false, cc, cfg.LoopUntil, cfg.parseOpts())
		if err != nil {
			return fmt.Errorf("%w: -loopUntil: %w", cli.ErrUsage, err)
		}
	}
	i := 0
	last := ir.Null()
	ticker := time.NewTicker(cfg.LoopEvery)
	defer ticker.Stop()
	diffCount := 0
	for {
		if i == cfg.LoopLim {
			if until != nil {
				// the loop ran out before the condition held, which is a
				// failure of what was asked for and not of the command
				return fmt.Errorf("-loopUntil did not match in %d iterations", i)
			}
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
		// checked after the difference is written, so that the change which
		// satisfied the condition is the last thing reported
		if until != nil && next != nil {
			done, err := tony.Match(next, until)
			if err != nil {
				return fmt.Errorf("error matching -loopUntil: %w", err)
			}
			if done {
				return nil
			}
		}
		last = next
		<-ticker.C
		i++
	}
	return nil
}

func diffInputs(do *DiffConfig, cc *cli.Context, a, b *ir.Node, sep bool) (bool, error) {
	// Diff is blind to comments unless asked, so with -c the question changes
	// from "do these hold the same thing" to "are these the same document".
	// Without it a comment-only change is no change, and diff answers with
	// nothing and an exit code of 0.
	d := tony.DiffWith(a, b, tony.DiffComments(do.Comments))
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
	if err := encode.Encode(d, w, do.encOpts(w)...); err != nil {
		return false, err
	}
	return true, nil
}
