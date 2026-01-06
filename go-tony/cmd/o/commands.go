package main

import (
	"time"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/ir"
)

func MainCommand() *cli.Command {
	cfg := &MainConfig{}
	sOpts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	opts := append(sOpts, []*cli.Opt{
		&cli.Opt{
			Name:        "o",
			Description: "output file (default stdout)",
			Type:        cli.NamedFuncOpt(cfg.outOpt, "(filepath)"),
		},
		&cli.Opt{
			Name:        "I",
			Aliases:     []string{"ifmt"},
			Description: "input format: tony/t, json/j, yaml/y",
			Type:        cli.NamedFuncOpt(cfg.fmtFunc(&cfg.InFormat), "(format)"),
		}, &cli.Opt{
			Name:        "O",
			Aliases:     []string{"ofmt"},
			Description: "output format: tony/t, json/j, yaml/y",
			Type:        cli.NamedFuncOpt(cfg.fmtFunc(&cfg.OutFormat), "(format)"),
		}}...)

	return cli.NewCommandAt(&cfg.Main, "o").
		WithSynopsis("o [opts] command [opts]").
		WithDescription("o is a tool for working with object notation.").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return oMain(cfg, cc, args)
		}).
		WithSubs(
			ViewCommand(cfg),
			EvalCommand(cfg),
			DiffCommand(cfg),
			GetCommand(cfg),
			ListCommand(cfg),
			MatchCommand(cfg),
			PatchCommand(cfg),
			BuildCommand(cfg),
			DumpCommand(cfg),
			LoadCommand(cfg),
			SchemaCommand(cfg),
			SystemCommand(cfg))
}

func EvalCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &EvalConfig{MainConfig: mainCfg, Env: map[string]*ir.Node{}}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	opts = append(opts,
		&cli.Opt{
			Name: "e",
			Type: cli.NamedFuncOpt(cli.FuncOpt(envOptTypeFunc(cfg.Env)), "(path=val)"),
		})

	cmd := cli.NewCommand("eval").
		WithAliases("e", "ev").
		WithSynopsis("eval [-e path=val [ -e path2=val2 ]...] [files]").
		WithDescription("Evaluate objects with !eval tags").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return tonyEval(cfg, cc, args)
		})
	cfg.Eval = cmd
	return cmd
}

func envOptTypeFunc(env map[string]*ir.Node) func(cc *cli.Context, a string) (any, error) {
	return func(cc *cli.Context, a string) (any, error) {
		if err := envFunc(env, a); err != nil {
			return nil, err
		}
		return 0, nil
	}
}

func ViewCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &ViewConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	cmd := cli.NewCommand("view").
		WithAliases("v").
		WithOpts(opts...).
		WithSynopsis("view [files]").
		WithDescription("view object files with tags in color").
		WithRun(func(cc *cli.Context, args []string) error {
			return view(cfg, cc, args)
		})
	cfg.View = cmd
	return cmd
}

func GetCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &GetConfig{MainConfig: mainCfg}
	cmd := cli.NewCommand("get").
		WithAliases("g", "ge").
		WithSynopsis("get <objectpath> [files]").
		WithDescription("get objects elements from files").
		WithRun(func(cc *cli.Context, args []string) error {
			return get(cfg, cc, args)
		})
	cfg.Get = cmd
	return cmd
}

func ListCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &ListConfig{MainConfig: mainCfg}
	return cli.NewCommandAt(&cfg.List, "list").
		WithAliases("l").
		WithSynopsis("list <objectpath> [files]").
		WithDescription("list or query objects elements from files").
		WithRun(func(cc *cli.Context, args []string) error {
			return list(cfg, cc, args)
		})
}

func MatchCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &MatchConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Command, "match").
		WithAliases("m").
		WithSynopsis("match [opts] <matchobj> [files]").
		WithDescription("match objects documents with match documents").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return match(cfg, cc, args)
		})
}

func DiffCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &DiffConfig{MainConfig: mainCfg, LoopEvery: time.Second, LoopLim: -1}
	loopEveryOpt := &cli.Opt{
		Name: "loopEvery",
		Type: cli.FuncOpt(cfg.mkLoopEvery()),
	}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	opts = append(opts, loopEveryOpt)

	cmd := cli.NewCommand("diff").
		WithAliases("d", "di").
		WithOpts(opts...).
		WithSynopsis("diff a b or diff -loop <cmd>").
		WithDescription("diff object documents").
		WithRun(func(cc *cli.Context, args []string) error {
			return diff(cfg, cc, args)
		})
	cfg.Diff = cmd
	return cmd
}

func PatchCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &PatchConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	cmd := cli.NewCommand("patch").
		WithAliases("p", "pa").
		WithSynopsis("patch [opts] <patchobj> [files]").
		WithDescription("patch object documents").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return patch(cfg, cc, args)
		})
	cfg.Patch = cmd
	return cmd
}

func BuildCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &BuildConfig{MainConfig: mainCfg, Env: map[string]*ir.Node{}}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	opts = append(opts, &cli.Opt{
		Name: "e",
		Type: cli.NamedFuncOpt(cli.FuncOpt(envOptTypeFunc(cfg.Env)), "(path=val)"),
	})
	return cli.NewCommandAt(&cfg.Build, "build").
		WithAliases("b").
		WithSynopsis("build [dir] [-l] [-p profile ] [ env ]").
		WithDescription(buildDescription).
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return build(cfg, cc, args)
		})
}

const buildDescription = `build is a tool for building manifests.  

Build operates on a build directory, which defaults to the current directory.

Build Object

Build looks for a file called 'build.{tony,objects ,json}' containing a 
build description object in the following form:

  build:
    # env describes the variables that can be set.  It can be any object
    # notation yt understands: tony, objects , json
    # env can be overriden on the command line with '-e path=val' or 
    # '-- key1=val1 key2=val2 ...' or via the environmental variable YTOOL_ENV
    # which may contain a patch for the env, such as '{debug: false}'.
    env:
      debug: true
      object : my-namespace
      # ...
    
    # optional destination directory
    destDir: out

    # sources object what source documents to use 
    sources:
    - dir: source # finds all object files in source relative to current directory.
    - exec: helm template ../../helm/stuf

    # patches are applied to sources
    patchs:
    - if: .[debug]  # condition from env
      match: null  # condition on source document
      patch:
        # ...
      # also can be in a separate file
    - file: my-pathes.tony

Build then 
1. initialises its environment
2. evaluates the sources and patches object descriptions with the environment
3. produces the sources
3. runs the sources through the patches conditionally 
4. takes the results and evaluates them with the environment
5. outputs the result to .destDir or the command output

Environment

Build can have the environment set in 4 ways
1. in the build object file.
2. using '-e path=value'
3. using '-- path1=value1 path2=value2 ...'
4. setting an environment patch in the OS environment variable $YTOOL_ENV

Arguments take precedence over the environment and later arguments take
precedence over earlier ones. Both take precedence over the default environment
specified in the 'env:' field of the build description object.

Profiles

build can have profiles, which are patches to the environment.  To list
profiles associated with the build, run build -l.  To run with a profile, pass
-p <profile> where <profile> is either a name in the list from '-l' or a
filename containing a patch for the environment.  Profiles are expected to be
object files in a sub-directory called 'profiles'.

Show

build -s shows the environment and can be helpful for learning what build
options are available.`

func DumpCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &DumpConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Dump, "dump").
		WithSynopsis("dump [files]").
		WithDescription("dump IR").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return dump(cfg, cc, args)
		})
}

func LoadCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &LoadConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Load, "load").
		WithSynopsis("load [ir-files]").
		WithDescription("load IR files and render them").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return load(cfg, cc, args)
		})
}

func SystemCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &SystemConfig{MainConfig: mainCfg}
	return cli.NewCommandAt(&cfg.System, "system").
		WithSynopsis("system <subcommand>").
		WithDescription("system commands implementing TonyAPI components").
		WithAliases("sys").
		WithSubs(
			LogDCommand(cfg.MainConfig),
			DocDCommand(cfg.MainConfig),
			UpCommand(cfg.MainConfig))
}
