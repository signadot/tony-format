package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/scott-cotton/cli"
)

// Asking what a command does is not a misuse of it, and a tool which answers -h
// with `usage error: unknown option: "h"` has told a first-time reader they got it
// wrong before they got anything else. The help is the same text either way; only
// where it is written and what is exited with differ:
//
//	asked for   stdout, exit 0
//	a misuse    stderr, exit 2, and say which word was wrong
//
// -h is answered by every command because it lives on MainConfig, which each
// command's config embeds; helpAsked is the one line each of them runs.

// helpAsked writes cmd's help when -h was given, and reports whether it did, so a
// command can return without doing its work.
func helpAsked(cmd *cli.Command, cc *cli.Context, asked bool) bool {
	if !asked {
		return false
	}
	// A nil error is what tells Usage to write to stdout rather than stderr.
	cmd.Usage(cc, nil)
	return true
}

// noSuchCommand reports a command that does not exist, with the nearest one that
// does. A typo is the likeliest way to get here and the tool knows the whole list,
// so leaving the reader to find it themselves is a choice rather than a limit.
func noSuchCommand(root *cli.Command, cc *cli.Context, given string) error {
	root.Usage(cc, fmt.Errorf("%w: %q", cli.ErrNoSuchCommand, given))
	if did := nearestCommand(root, given); did != "" {
		fmt.Fprintf(cc.Err, "\ndid you mean `o %s`?\n", did)
	}
	return cli.ExitCodeErr(2)
}

// nearestCommand answers the command name closest to given, or "" when nothing is
// close enough to be worth suggesting. Names and aliases both count: a reader who
// typed `o mat` meant match whichever of the two they were reaching for.
func nearestCommand(root *cli.Command, given string) string {
	best, bestDist := "", len(given)/2+1
	for _, c := range root.Children {
		for _, name := range append([]string{c.Name}, c.Aliases...) {
			d := editDistance(given, name)
			if d < bestDist || (d == bestDist && best == "") {
				best, bestDist = c.Name, d
			}
		}
	}
	return best
}

// editDistance is Levenshtein, on the two short strings a command name and a typo
// of one are.
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// HelpCommand is `o help [command]`, for readers who reach for the word rather
// than the flag. Both spellings answer, because both are typed.
func HelpCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &HelpConfig{MainConfig: mainCfg}
	cmd := cli.NewCommand("help").
		WithSynopsis("help [command]").
		WithDescription("Show help for o, or for one of its commands").
		WithRun(func(cc *cli.Context, args []string) error {
			return help(cfg, cc, args)
		})
	cfg.Cmd = cmd
	return cmd
}

// HelpConfig configures `o help`. The command field is Cmd rather than Help so it
// does not shadow MainConfig.Help, which is the -h flag every command answers.
type HelpConfig struct {
	*MainConfig
	Cmd *cli.Command
}

func help(cfg *HelpConfig, cc *cli.Context, args []string) error {
	root := cfg.Main
	if len(args) == 0 {
		root.Usage(cc, nil)
		fmt.Fprintf(cc.Out, "\nrun `o help <command>` for one of them, or see %s\n", docsSite)
		return nil
	}
	sub := root.FindSub(cc, args[0])
	if sub == nil {
		return noSuchCommand(root, cc, args[0])
	}
	sub.Usage(cc, nil)
	return nil
}

// commandNames is every command and alias, sorted -- what a shell needs to
// complete, and what a reader needs when they have mistyped one.
func commandNames(root *cli.Command, withAliases bool) []string {
	var names []string
	for _, c := range root.Children {
		names = append(names, c.Name)
		if withAliases {
			names = append(names, c.Aliases...)
		}
	}
	sort.Strings(names)
	return names
}

// optionNames is every option a command accepts, its inherited ones included,
// each with its leading '-'.
func optionNames(cmd *cli.Command) []string {
	var names []string
	for name := range cmd.AllOpts() {
		names = append(names, "-"+name)
	}
	sort.Strings(names)
	return names
}

// quoteForShell is the one escaping a generated completion script needs: the
// names are Go identifiers and flags, so only the quote itself can surprise it.
func quoteForShell(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}
