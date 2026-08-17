package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/scott-cotton/cli"
)

// DocsConfig configures `o docs`.
type DocsConfig struct {
	*MainConfig
	Site string `cli:"name=site desc='base URL of the published docs, for cross-page links'"`
	Docs *cli.Command
}

// docsSite is where these pages are published. A generated page links to the
// site page of each command it names, and the terms below link into the rest of
// the documentation.
const docsSite = "https://signadot.github.io/tony-format/"

// docsTerms are words which, appearing in a command's synopsis or description,
// become links to the page which explains them. Only the first occurrence in
// each description is linked, so a term repeated in a sentence does not turn
// into a row of links.
//
// Keep these to things a reader would otherwise have to go looking for. A term
// which appears in nearly every description -- "object", say -- links
// everything and so distinguishes nothing.
// "tony" is deliberately absent. It occurs in three inherited option descriptions
// -- "do i/o in tony", "input format: tony/t", "output format: tony/t" -- which
// repeat on every page, so linking it produced 71 links inside option tables and
// one in prose. A link in a dense table of flags tells a reader nothing they were
// looking for.
var docsTerms = cli.Terms{
	"schema": "/tonyschema/",
	"logd":   "/logd/",
	"docd":   "/docd/",
	"IR":     "/ir/",
	// "match document" and "patch object" are deliberately absent, for the reason
	// "tony" is: they appear in the -if, -if-file and -trim descriptions, which
	// repeat as three rows of one option table, and a link in a dense table of
	// flags tells a reader nothing they were looking for. diff, match and patch
	// name the page in their descriptions instead, where a reader is reading.
}

// DocsCommand writes this command tree as markdown.
//
// The tree documents itself because it is the only thing that has it:
// MainCommand lives in package main, so nothing outside this binary can walk
// it. Running the tool is therefore how its documentation is produced, which
// also means the docs cannot describe a command the binary does not have.
func DocsCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &DocsConfig{MainConfig: mainCfg, Site: docsSite}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	cmd := cli.NewCommand("docs").
		WithSynopsis("docs <dir>").
		WithDescription("Write this command tree as markdown, one page per command").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return writeDocs(cfg, cc, args)
		})
	cfg.Docs = cmd
	return cmd
}

// sitePage says where the page this run writes for a command is published, given
// the directory being written into.
//
// cli.DefaultPage answers the command path -- "o build" is published at "o/build/"
// -- but the file it writes for that command is "o-build.md", which mkdocs
// publishes at "o/o-build/". Taking the default leaves every generated page
// linking to a URL nothing occupies, and invites a second, hand-written page to
// fill it: that is how docs/o/build.md came to duplicate this one.
//
// So the link is derived from the file rather than the command. dir is the
// directory passed to `o docs`, whose name is the one path segment the file name
// does not carry.
func sitePage(dir string) func(*cli.Command) string {
	return func(cmd *cli.Command) string {
		names := make([]string, 0, 4)
		for _, c := range cmd.Path() {
			names = append(names, c.Name)
		}
		// The root command is written to README.md, which is the directory itself.
		if len(names) < 2 {
			return dir + "/"
		}
		return dir + "/" + strings.Join(names, "-") + "/"
	}
}

func writeDocs(cfg *DocsConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Docs.Parse(cc, args)
	if err != nil {
		cfg.Docs.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	if helpAsked(cfg.Docs, cc, cfg.Help) {
		return nil
	}
	if len(args) != 1 {
		return usageErr(cfg.Docs, cc, "docs takes one argument: the directory to write")
	}
	doc := cli.NewDoc(MainCommand()).
		WithSite(cfg.Site).
		WithPage(sitePage(filepath.Base(args[0]))).
		WithTerms(docsTerms)
	if err := doc.Write(args[0]); err != nil {
		return fault(cc, err)
	}
	fmt.Fprintf(cc.Out, "wrote %s\n", args[0])
	return nil
}
