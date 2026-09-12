package main

import (
	"fmt"

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
		WithDescription("Write this command tree as markdown, one page per command.").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return writeDocs(cfg, cc, args)
		})
	cfg.Docs = cmd
	return cmd
}

// noSitePage says that no command has a page on the site apart from the one this
// run writes.
//
// cli.Doc links each command's page to "the published documentation" for that
// command, which is for a markdown copy living somewhere other than the site.
// These files are the site: mkdocs publishes docs/o/ as it is, so that link took
// every page to itself. Site is still set, since the term links hang off it.
func noSitePage(*cli.Command) string { return "" }

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
		WithPage(noSitePage).
		WithTerms(docsTerms)
	if err := doc.Write(args[0]); err != nil {
		return fault(cc, err)
	}
	fmt.Fprintf(cc.Out, "wrote %s\n", args[0])
	return nil
}
