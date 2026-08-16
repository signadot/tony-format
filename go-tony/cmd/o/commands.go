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
			SystemCommand(cfg),
			DocsCommand(cfg),
			HelpCommand(cfg),
			CompletionCommand(cfg),
			VersionCommand())
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

// parseEnvExtras parses "-- key=val ..." arguments and adds them to env.
// Returns the args before the "--" delimiter.
func parseEnvExtras(env map[string]*ir.Node, cc *cli.Context, args []string) ([]string, error) {
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
	f := envOptTypeFunc(env)
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

const getDesc = `get objects elements from files

The path is a kpath, the syntax the rest of the system uses: .field steps into an
object, [i] into an array, {i} into a sparse one, and (key) into a keyed array by
identity, so "o get 'items(WIDGET).qty'" names an element wherever it sits. A
leading $ is accepted and dropped, from when paths were written that way.

The path says WHERE to look. -if says WHICH of what is there to keep: the node
is written only when it matches the match document given, so

    o get -if '{state: open}' '$.items[0]' doc.tony && deploy

reads as the guard it is. -trim writes only the parts its own match document
names. A file is optional: with none, stdin is read.

An input is a STREAM of documents, --- separated, and the path is asked of each
one -- which is what makes the output of one command the input of the next:

    o get .spec a.tony b.tony | o get .replicas

Exit codes are the search convention: 0 when something was written, 1 when the
path named nothing or what it named did not match, 2 for a fault.

The match documents -if and -trim take are the ones o match takes; the operators
they may use are at https://signadot.github.io/tony-format/matchpatch/`

func GetCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &GetConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	cmd := cli.NewCommand("get").
		WithAliases("g", "ge").
		WithSynopsis("get [opts] <kpath> [files]").
		WithDescription(getDesc).
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return get(cfg, cc, args)
		})
	cfg.Get = cmd
	return cmd
}

const listDesc = `list or query objects elements from files

The path is a kpath -- .field, [i], {i}, (key), and the wildcards .* [*] {*}, which
belong here rather than in get: list answers with every node the path names. A
leading $ is accepted and dropped, from when paths were written that way.

The path says WHERE to look and -if says WHICH of what is there to keep, which
together are how a list is filtered by a match:

    x | o list -if '{state: open}' '$[*]'            # the matching elements
    o list -if '{state: open}' '$.items[*]' doc.tony # at any depth the path reaches

-trim writes only the parts its own match document names, so which nodes and how
much of each are asked separately:

    x | o list -if '{status: running}' -trim '{runner: null, started: null}' '[*]'

A file is optional: with none, stdin is read, as grep and cat do -- from a pipe,
or typed at a terminal and ended with Ctrl-D. An input is a STREAM of documents,
--- separated, and the path is asked of every one of them; the answer is a
single list over all of them, whichever input each node came from.

Without -if the path is the whole question and every node it names is written.
The answer is a list, and the empty list is written as one; the exit code says
whether it was empty: 0 when something was kept, 1 when nothing was, 2 for a
fault.

The match documents -if and -trim take are the ones o match takes; the operators
they may use are at https://signadot.github.io/tony-format/matchpatch/`

func ListCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &ListConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.List, "list").
		WithAliases("l").
		WithSynopsis("list [opts] <kpath> [files]").
		WithDescription(listDesc).
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return list(cfg, cc, args)
		})
}

const matchDesc = `match objects documents with match documents

Each document is matched whole: a file holding a list is one document, and the
pattern is asked about the list rather than about its elements. A file holding
several documents separated by --- is matched one at a time, and the ones which
match are written, so match reads as a filter over a document stream.

A file is optional: with none, stdin is read, so "x | o m PAT" needs no
trailing -.

Exit codes follow grep, so that a pipe can tell an answer from a fault:

  0  something matched and was written
  1  nothing matched -- an answer, not an error, and written on no stream
  2  a fault: bad usage, unreadable input, an unparseable match document

o match -tags lists the operators a match may use, from this binary. What each one
means, and how match and patch share a vocabulary, is at
https://signadot.github.io/tony-format/matchpatch/`

func MatchCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &MatchConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Command, "match").
		WithAliases("m").
		WithSynopsis("match [opts] <matchobj> [files]").
		WithDescription(matchDesc).
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return match(cfg, cc, args)
		})
}

func DiffCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &DiffConfig{MainConfig: mainCfg, LoopEvery: time.Second, LoopLim: -1}
	loopEveryOpt := &cli.Opt{
		Name:        "loopEvery",
		Description: "how long to wait between runs of the looped command",
		Type:        cli.FuncOpt(cfg.mkLoopEvery()),
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
		WithDescription(diffDescription).
		WithRun(func(cc *cli.Context, args []string) error {
			return diff(cfg, cc, args)
		})
	cfg.Diff = cmd
	return cmd
}

const diffDescription = `diff object documents

Given two arguments, diff writes what turns the first into the second, and
exits 1 if they differ at all. Exit 2 is a fault -- a file that cannot be read is
not a difference.

Given one, the other is standard input: "o diff baseline.tony" reads as what turns
the baseline into what was piped in, which is the order diff writes anyway. Naming
it with - still works, and is what to write when stdin is the FIRST operand. Both
cannot be left out, because a document does not differ from itself.

Loop Mode

With -loop <cmd>, diff runs the command every -loopEvery and writes what
changed since the run before, until -loopLim runs have happened.  The command
is expected to write an object; watching one is what this is for.

-loopUntil <match> stops the loop once the command's output matches, which
makes diff a way to wait for a state and see how it got there:

    o d -loop 'kubectl get pod x -o yaml' -loopUntil '{status: {phase: Running}}'

The match is a match object, as taken by 'o match', so it need only name the
fields it cares about, and the operators it may use are at
https://signadot.github.io/tony-format/matchpatch/

It is matched against what the command wrote and not
against the difference, and it is checked after that difference is written, so
the change which satisfied it is the last thing reported.  Should the loop hit
-loopLim first, diff exits 1: the condition asked for did not hold.`

const patchDesc = `patch object documents

The patch is applied to every document of every input and each result is written,
--- separated.

Files are optional: with none, stdin is read, as grep and cat do. An input is a
STREAM of documents, so a pipeline is written the obvious way:

    o get .spec a.tony b.tony | o patch '{replicas: 3}'

Without -c the result carries no comments -- not the patch's, and not the ones
the document being patched already had -- because a patch answers with data.

A patch which deletes a whole document writes nothing for it, which is the result
and not a fault. Exit codes: 0, and 2 for a fault.

o patch -tags lists the operators a patch may use, from this binary. What each one
means is at https://signadot.github.io/tony-format/matchpatch/`

func PatchCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &PatchConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	cmd := cli.NewCommand("patch").
		WithAliases("p", "pa").
		WithSynopsis("patch [opts] <patchobj> [files]").
		WithDescription(patchDesc).
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
	opts = append(opts, &cli.Opt{
		Name:        "p",
		Aliases:     []string{"profile"},
		Description: "profile(s) to build (can be specified multiple times)",
		Type: cli.NamedFuncOpt(cli.FuncOpt(func(_ *cli.Context, v string) (any, error) {
			cfg.Profiles = append(cfg.Profiles, v)
			return v, nil
		}), "profile"),
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
      # '-- key1=val1 key2=val2 ...' or via the environmental variable TONY_DIRBUILD_ENV
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

Build then:

1. initialises its environment
2. evaluates the sources and patches object descriptions with the environment
3. produces the sources
4. runs the sources through the patches conditionally
5. takes the results and evaluates them with the environment
6. outputs the result to .destDir or the command output

Environment

Build can have the environment set in 4 ways:

1. in the build object file.
2. using '-e path=value'
3. using '-- path1=value1 path2=value2 ...'
4. setting an environment patch in the OS environment variable $TONY_DIRBUILD_ENV

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
