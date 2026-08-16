package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"

	"github.com/scott-cotton/cli"

	"github.com/mattn/go-isatty"
)

type MainConfig struct {
	B       bool `cli:"name=b desc='encode with brackets'"`
	X       bool `cli:"name=x desc='expand <<: merge field while encoding'"`
	Color   bool `cli:"name=color desc='encode with color'"`
	WireOut bool `cli:"name=wire desc='output in compact format'"`

	// Help is answered by every command, because -h is the first thing anyone
	// types and the last thing a tool should argue with. See helpAsked.
	Help bool `cli:"name=h aliases=help desc='show help for this command'"`

	T bool `cli:"name=t aliases=tony desc='do i/o in tony'"`
	J bool `cli:"name=j aliases=json desc='do i/o in json'"`
	Y bool `cli:"name=y aliases=yaml desc='do i/o in yaml'"`

	InFormat, OutFormat *format.Format

	Out      string
	CloseOut func() error

	Main *cli.Command
}

func (cfg *MainConfig) fmtFunc(fps ...**format.Format) cli.FuncOpt {
	return cli.FuncOpt(func(_ *cli.Context, v string) (any, error) {
		f, err := format.ParseFormat(v)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", cli.ErrUsage, err)
		}
		for _, fp := range fps {
			*fp = &f
		}
		return f, nil
	})
}

func (cfg *MainConfig) parseOpts() []parse.ParseOption {
	var fmat format.Format
	switch {
	case cfg.T:
		fmat = format.TonyFormat
	case cfg.Y:
		fmat = format.YAMLFormat
	case cfg.J:
		fmat = format.JSONFormat
	}
	if cfg.InFormat != nil {
		fmat = *cfg.InFormat
	}

	res := []parse.ParseOption{
		parse.ParseFormat(fmat),
	}
	// it would be nicer if cli supported
	// pointers to builtin types as well...
	brkts := false
	brktsSet := false
	for _, opt := range cfg.Main.Opts {
		if opt.Name != "b" {
			continue
		}
		brktsSet = opt.Value != nil
		if brktsSet {
			brkts = (*opt.Value).(bool)
		}
		break
	}
	if !brkts && brktsSet {
		res = append(res, parse.NoBrackets())
	}
	return res
}

func (cfg *MainConfig) encOpts(w io.Writer) []encode.EncodeOption {
	var fmt format.Format
	switch {
	case cfg.T:
		fmt = format.TonyFormat
	case cfg.Y:
		fmt = format.YAMLFormat
	case cfg.J:
		fmt = format.JSONFormat
	}
	if cfg.OutFormat != nil {
		fmt = *cfg.OutFormat
	}
	res := []encode.EncodeOption{
		encode.InjectRaw(cfg.X),
		encode.EncodeFormat(fmt),
		encode.EncodeWire(cfg.WireOut),
		encode.EncodeBrackets(cfg.B),
	}
	if cfg.Color {
		res = append(res, encode.EncodeColors(encode.NewColors()))
		return res
	}
	colorsSet := false
	for _, opt := range cfg.Main.Opts {
		if opt.Name != "color" {
			continue
		}
		colorsSet = opt.Value != nil
		break
	}
	if colorsSet {
		return res
	}
	f, ok := w.(*os.File)
	if !ok {
		return res
	}
	if isatty.IsTerminal(f.Fd()) {
		res = append(res, encode.EncodeColors(encode.NewColors()))
		return res
	}
	return res
}

type EvalConfig struct {
	*MainConfig
	Env  map[string]*ir.Node
	Tags bool `cli:"name=tags desc='show available tags'"`

	Eval *cli.Command
}

type ViewConfig struct {
	*MainConfig

	Comments bool `cli:"name=c desc='include comments'"`
	View     *cli.Command
}

func (cfg *ViewConfig) parseOpts() []parse.ParseOption {
	return append(cfg.MainConfig.parseOpts(), parse.ParseComments(cfg.Comments))
}

type GetConfig struct {
	*MainConfig

	Comments bool   `cli:"name=c desc='include comments'"`
	If       string `cli:"name=if desc='keep the node only if it matches this match document'"`
	IfFile   string `cli:"name=if-file desc='read the match document from a file'"`
	Trim     string `cli:"name=trim desc='write only the parts this match document names'"`

	Get *cli.Command
}

func (cfg *GetConfig) parseOpts() []parse.ParseOption {
	return append(cfg.MainConfig.parseOpts(), parse.ParseComments(cfg.Comments))
}

func (cfg *GetConfig) encOpts(w io.Writer) []encode.EncodeOption {
	return withComments(cfg.MainConfig.encOpts(w), cfg.Comments)
}

type ListConfig struct {
	*MainConfig

	Comments bool   `cli:"name=c desc='include comments'"`
	If       string `cli:"name=if desc='keep only the nodes matching this match document'"`
	IfFile   string `cli:"name=if-file desc='read the match document from a file'"`
	Trim     string `cli:"name=trim desc='write only the parts this match document names'"`

	List *cli.Command
}

func (cfg *ListConfig) parseOpts() []parse.ParseOption {
	return append(cfg.MainConfig.parseOpts(), parse.ParseComments(cfg.Comments))
}

func (cfg *ListConfig) encOpts(w io.Writer) []encode.EncodeOption {
	return withComments(cfg.MainConfig.encOpts(w), cfg.Comments)
}

type MatchConfig struct {
	*cli.Command
	*MainConfig

	Comments bool `cli:"name=c desc='include comments in the answer; matching is blind to them either way'"`
	Trim     bool `cli:"name=trim desc='trim the results to the match'"`
	String   bool `cli:"name=s desc='consider match a string argument'"`
	File     bool `cli:"name=f desc='consider match a file path'"`
	Tags     bool `cli:"name=tags desc='show available tags'"`
}

// parseOpts reads comments when asked. Matching itself stays blind to them
// either way -- a match asks about the value, and sees through what was said
// about it -- so this decides what a -trim result can carry, not what matches.
func (cfg *MatchConfig) parseOpts() []parse.ParseOption {
	return append(cfg.MainConfig.parseOpts(), parse.ParseComments(cfg.Comments))
}

func (cfg *MatchConfig) encOpts(w io.Writer) []encode.EncodeOption {
	return withComments(cfg.MainConfig.encOpts(w), cfg.Comments)
}

type DiffConfig struct {
	*MainConfig
	Comments  bool   `cli:"name=c desc='include comments, and report differences in them'"`
	Reverse   bool   `cli:"name=r desc='reverse the diff'"`
	Loop      string `cli:"name=loop desc='command to produce objects to diff in a loop'"`
	LoopEvery time.Duration
	LoopLim   int    `cli:"name=loopLim desc='max number of times to loop'"`
	LoopUntil string `cli:"name=loopUntil desc='stop once the looped command output matches this match object'"`

	Diff *cli.Command
}

func (cfg *DiffConfig) parseOpts() []parse.ParseOption {
	return append(cfg.MainConfig.parseOpts(), parse.ParseComments(cfg.Comments))
}

func (cfg *DiffConfig) encOpts(w io.Writer) []encode.EncodeOption {
	return withComments(cfg.MainConfig.encOpts(w), cfg.Comments)
}

// withComments adds comments to an encoding when they were asked for. Reading
// them is not enough on its own: a document parsed with comments and encoded
// without them comes out stripped, which is the same answer as not having read
// them and a confusing way to give it.
func withComments(opts []encode.EncodeOption, on bool) []encode.EncodeOption {
	if !on {
		return opts
	}
	return append(opts, encode.EncodeComments(true))
}

func (cfg *DiffConfig) mkLoopEvery() func(cc *cli.Context, a string) (any, error) {
	return func(_ *cli.Context, a string) (any, error) {
		d, err := time.ParseDuration(a)
		if err != nil {
			return nil, err
		}
		cfg.LoopEvery = d
		return d, nil
	}
}

type PatchConfig struct {
	*MainConfig
	Comments bool `cli:"name=c desc='include comments, of the document as well as the patch'"`
	Reverse  bool `cli:"name=r desc='apply diff reversed'"`
	String   bool `cli:"name=s desc='patch arg as string'"`
	File     bool `cli:"name=f desc='patch arg as file'"`
	Tags     bool `cli:"name=tags desc='show available tags'"`

	Patch *cli.Command
}

func (cfg *PatchConfig) parseOpts() []parse.ParseOption {
	return append(cfg.MainConfig.parseOpts(), parse.ParseComments(cfg.Comments))
}

func (cfg *PatchConfig) encOpts(w io.Writer) []encode.EncodeOption {
	return withComments(cfg.MainConfig.encOpts(w), cfg.Comments)
}

type BuildConfig struct {
	*MainConfig
	Env map[string]*ir.Node

	List     bool `cli:"name=l aliases=list desc='list profiles'"`
	Profiles []string
	ShowEnv  bool `cli:"name=s aliases=show,sh desc='show environment'"`

	Build *cli.Command
}

type DumpConfig struct {
	*MainConfig
	Comments bool `cli:"name=c desc='include comments'"`
	Dump     *cli.Command
}

func (cfg *DumpConfig) parseOpts() []parse.ParseOption {
	return append(cfg.MainConfig.parseOpts(), parse.ParseComments(cfg.Comments))
}

type LoadConfig struct {
	*MainConfig
	Comments bool `cli:"name=c desc='include comments'"`
	Load     *cli.Command
}

func (cfg *LoadConfig) parseOpts() []parse.ParseOption {
	return append(cfg.MainConfig.parseOpts(), parse.ParseComments(cfg.Comments))
}

type SystemConfig struct {
	*MainConfig
	System *cli.Command
}
