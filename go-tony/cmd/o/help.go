package main

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

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
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	cmd := cli.NewCommand("help").
		WithSynopsis("help [command]").
		WithDescription("Show help for o, or for one of its commands.").
		WithOpts(opts...).
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
	args, err := cfg.Cmd.Parse(cc, args)
	if err != nil {
		cfg.Cmd.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	// `o help -h` is help for help, which is a fair thing to ask of the command
	// whose whole subject is help.
	if helpAsked(cfg.Cmd, cc, cfg.Help) {
		return nil
	}
	root := cfg.Main
	if len(args) == 0 {
		listCommands(root, cc)
		fmt.Fprint(cc.Out, conventions)
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

// listCommands writes the two things a reader needs in order to choose a command: what it
// does, and how to call it.
//
// The library's own listing gives the synopsis alone. That answers the second question and
// not the first, since `view [file...]`, `dump [file...]` and `load [ir-file...]` are three
// different jobs behind three identical shapes. Each command is therefore written as what
// it does, and then as how it is spelled.
//
// The synopsis is kept because a reader who cannot see the arguments will guess at them,
// and the order is the easiest thing to guess wrong. A command which takes an argument
// takes it before the files, and `<kpath>` written next to `[file...]` says so.
func listCommands(root *cli.Command, cc *cli.Context) {
	fmt.Fprintf(cc.Out, "%s\n\n%s\n\ncommands:\n", root.Synopsis, root.Description)
	tw := tabwriter.NewWriter(cc.Out, 1, 4, 2, ' ', 0)
	for _, c := range root.Children {
		fmt.Fprintf(tw, "  %s\t%s\n", c.Name, summarize(c))
		fmt.Fprintf(tw, "  \t%s\n", c.Synopsis)
	}
	tw.Flush()
}

// summarize is a command's first line of description, which is written as the one-line
// answer to what the command is for. A description opening with a longer paragraph is cut
// at its first sentence rather than wrapped, since this is a table.
func summarize(c *cli.Command) string {
	line, _, _ := strings.Cut(strings.TrimSpace(c.Description), "\n")
	if s, _, ok := strings.Cut(line, ". "); ok {
		line = s
	}
	return strings.TrimSuffix(strings.TrimSpace(line), ".")
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

// A command whose job is to hold others -- schema, system -- answers the same way
// the root does: -h and no argument at all print its help on stdout and exit 0, and
// a name it does not have is a misuse, with the nearest one it does offered.
//
// They used to answer neither. `o schema -h` exited 1 with `unknown option: "h"`,
// because a group command registers no options of its own, and `o schema` exited 1
// on the library's ErrNoCommandProvided -- so the two ways of asking a group what
// it holds were both errors.
func groupRun(group *cli.Command, cfg *MainConfig, cc *cli.Context, args []string) error {
	args, err := group.Parse(cc, args)
	if err != nil {
		group.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	if cfg.Help || len(args) == 0 {
		group.Usage(cc, nil)
		if !cfg.Help {
			fmt.Fprintf(cc.Out, "\nplease choose a subcommand of `o %s`.\n", group.Name)
		}
		return nil
	}
	sub := group.FindSub(cc, args[0])
	if sub == nil {
		return noSuchSub(group, cc, args[0])
	}
	return sub.Run(cc, args[1:])
}

// noSuchSub is noSuchCommand for a subcommand of a group.
func noSuchSub(group *cli.Command, cc *cli.Context, given string) error {
	group.Usage(cc, fmt.Errorf("%w: %q", cli.ErrNoSuchCommand, given))
	if did := nearestCommand(group, given); did != "" {
		fmt.Fprintf(cc.Err, "\ndid you mean `o %s %s`?\n", group.Name, did)
	}
	return cli.ExitCodeErr(2)
}

// conventions states what holds across the commands which read documents. A reader would
// otherwise infer it from as many help pages as it takes to notice the pattern. It is
// written once rather than repeated because it does not vary, and because the cost of not
// knowing it is a wrong call rather than a confusing one: an argument given in the wrong
// place is read as a filename, and reported as a missing file.
const conventions = `
The commands that read documents share a few conventions.

  A command which takes an argument takes it before any files:
      o get .spec f.tony        o patch '{replicas: 3}' f.tony
  With no files, standard input is read, so the commands compose:
      o get .spec a.tony | o patch '{replicas: 3}'
  An input is a stream of documents. Every document in it is read, and
  every answer is written.
  The exit code is 0 when something was answered, 1 when nothing was
  found (for diff, that the two documents differ), and 2 for a fault.
`
