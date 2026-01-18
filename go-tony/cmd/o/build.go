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
	args, err = parseEnvExtras(cfg.Env, cc, args)
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
	for _, profile := range cfg.Profiles {
		if profile == "-" {
			data, err := io.ReadAll(cc.In)
			if err != nil {
				return fmt.Errorf("error reading profile from stdin: %w", err)
			}
			if err := dir.LoadProfileFromBytes(data, eval.EnvToMapAny(cfg.Env)); err != nil {
				return fmt.Errorf("error loading profile from stdin: %w", err)
			}
		} else {
			if err := dir.LoadProfile(profile, eval.EnvToMapAny(cfg.Env)); err != nil {
				return fmt.Errorf("error loading profile %s: %w", profile, err)
			}
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
