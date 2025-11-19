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

	Get *cli.Command
}

type ListConfig struct {
	*MainConfig

	List *cli.Command
}

type MatchConfig struct {
	*cli.Command
	*MainConfig

	Trim   bool `cli:"name=trim desc='trim the results to the match'"`
	String bool `cli:"name=s desc='consider match a string argument'"`
	File   bool `cli:"name=f desc='consider match a file path'"`
	Tags   bool `cli:"name=tags desc='show available tags'"`
}

type DiffConfig struct {
	*MainConfig
	Reverse   bool   `cli:"name=r desc='reverse the diff'"`
	Loop      string `cli:"name=loop desc='command to produce objects to diff in a loop'"`
	LoopEvery time.Duration
	LoopLim   int `cli:"name=loopLim desc='max number of times to loop'"`

	Diff *cli.Command
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
	Reverse bool `cli:"name=r desc='apply diff reversed'"`
	String  bool `cli:"name=s desc='patch arg as string'"`
	File    bool `cli:"name=f desc='patch arg as file'"`
	Tags    bool `cli:"name=tags desc='show available tags'"`

	Patch *cli.Command
}

type BuildConfig struct {
	*MainConfig
	Env map[string]*ir.Node

	List    bool   `cli:"name=l aliases=list desc='list profiles'"`
	Profile string `cli:"name=p aliases=profile desc='profile to build'"`
	ShowEnv bool   `cli:"name=s aliases=show,sh desc='show environment'"`

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
