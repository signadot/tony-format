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
		cfg.Build.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	if helpAsked(cfg.Build, cc, cfg.Help) {
		return nil
	}
	if err := cfg.oneFormat(cfg.Build, cc); err != nil {
		return err
	}
	args, err = parseEnvExtras(cfg.Env, cc, args)
	if err != nil {
		return usageErr(cfg.Build, cc, err.Error())
	}
	dirPath := "."
	if len(args) != 0 {
		dirPath = args[0]
	}
	dir, err := dirbuild.OpenDir(dirPath, cfg.Env)
	if err != nil {
		return fault(cc, err)
	}
	if cfg.ShowEnv && cfg.List {
		return usageErr(cfg.Build, cc, "cannot use -s and -l together")
	}
	if cfg.List {
		profiles, err := dir.Profiles()
		if err != nil {
			return fault(cc, fmt.Errorf("error getting profiles: %w", err))
		}
		for _, profile := range profiles {
			fmt.Fprintln(cc.Out, profile)
		}
		return nil
	}
	var w io.WriteCloser = cc.Out
	if dir.Output.DestDir != "" && cfg.Out == "" {
		w = nil
	}
	if w != nil {
		dir.Output.DestDir = ""
	}
	for _, profile := range cfg.Profiles {
		if profile == "-" {
			data, err := io.ReadAll(cc.In)
			if err != nil {
				return fault(cc, fmt.Errorf("error reading profile from stdin: %w", err))
			}
			if err := dir.LoadProfileFromBytes(data, eval.EnvToMapAny(cfg.Env)); err != nil {
				return fault(cc, fmt.Errorf("error loading profile from stdin: %w", err))
			}
		} else {
			if err := dir.LoadProfile(profile, eval.EnvToMapAny(cfg.Env)); err != nil {
				return fault(cc, fmt.Errorf("error loading profile %s: %w", profile, err))
			}
		}
	}
	if cfg.ShowEnv {
		opts := append(cfg.MainConfig.encOpts(cc.Out), encode.EncodeComments(true))
		if err := encode.Encode(ir.Comment(ir.FromMap(dir.Env), "# build environment:"), cc.Out, opts...); err != nil {
			return fault(cc, err)
		}
		return nil
	}
	_, err = dir.Run(w, cfg.MainConfig.encOpts(w)...)
	if err != nil {
		return fault(cc, err)
	}
	return nil
}
