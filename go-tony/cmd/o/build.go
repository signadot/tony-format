package main

import (
	"fmt"
	"io"

	"github.com/signadot/tony-format/go-tony/dirbuild"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/ir"

	"github.com/scott-cotton/cli"
)

func build(cfg *BuildConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Build.Parse(cc, args)
	if err != nil {
		return err
	}
	args, err = parseEnvExtras(cfg, cc, args)
	if err != nil {
		return err
	}
	dirPath := "."
	if len(args) != 0 {
		dirPath = args[0]
	}
	dir, err := dirbuild.OpenDir(dirPath, cfg.Env)
	if err != nil {
		return err
	}
	if cfg.ShowEnv && cfg.List {
		return fmt.Errorf("%w: cannot use -s and -l together", cli.ErrUsage)
	}
	if cfg.List {
		profiles, err := dir.Profiles()
		if err != nil {
			return fmt.Errorf("error getting profiles: %w", err)
		}
		for _, profile := range profiles {
			fmt.Fprintln(cc.Out, profile)
		}
		return nil
	}
	var w io.WriteCloser = cc.Out
	if dir.DestDir != "" && cfg.Out == "" {
		w = nil
	}
	if w != nil {
		dir.DestDir = ""
	}
	if cfg.Profile != "" {
		if err := dir.LoadProfile(cfg.Profile, eval.EnvToMapAny(cfg.Env)); err != nil {
			return fmt.Errorf("error loading profile %s: %w", cfg.Profile, err)
		}
	}
	if cfg.ShowEnv {
		opts := append(cfg.MainConfig.encOpts(cc.Out), encode.EncodeComments(true))
		return encode.Encode(ir.Comment(ir.FromMap(dir.Env), "# build environment:"), cc.Out, opts...)
	}
	_, err = dir.Run(w, cfg.MainConfig.encOpts(w)...)
	if err != nil {
		return err
	}
	return nil
}

func parseEnvExtras(cfg *BuildConfig, cc *cli.Context, args []string) ([]string, error) {
	delim := -1
	for i, arg := range args {
		if arg == "--" {
			delim = i
			break
		}
	}
	if delim == -1 {
		return args, nil
	}
	f := envOptTypeFunc(cfg.Env)
	ret := args[:delim]
	delim++
	for delim < len(args) {
		arg := args[delim]
		delim++
		_, err := f(cc, arg)
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}
