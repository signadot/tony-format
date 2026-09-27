package commands

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

// The working set, for the commands. It is the set the MCP server serves
// (mcp_workspace.go): the repositories ~/.config/git-issue.tony names, and
// the one the command is run in. A command runs on one repository of it, and
// which one is settled where commands are dispatched, before the command
// runs (bqgb1c8zh12ksh2xq1n0):
//
//   - --repo names it;
//   - a command given an issue runs on the repository that holds the issue --
//     the one it is run in when that holds it, as before there was a set;
//   - a command that makes one thing in one place -- create, import, ext add,
//     serve -- runs on the repository it is run in, and outside one needs
//     --repo unless the set is one repository;
//   - a command that reads or syncs and is given nothing -- list, watch, pull,
//     push, ext list, ext refresh -- outside a repository covers every
//     repository of the set: list and watch together, the rest each in turn.
//
// The commands are written against a Store and Root shares one among them, so
// the repository is settled by pointing that store (targetStore).

// targetStore is the store the commands share: the repository the command
// being run is on.
type targetStore struct {
	issuelib.Store

	repo  *repo   // the repository, when the set settled it
	every []*repo // for a command over every repository run outside one: the set
	each  []*repo // for a command run on each repository in turn: the set
	set   func() (*workspace, error)
}

// targets answers the repositories a command over every repository covers:
// the one its store is on, unless it was run outside a repository with a set.
func targets(st issuelib.Store) []*repo {
	if t, ok := st.(*targetStore); ok && len(t.every) > 0 {
		return t.every
	}
	return []*repo{{Store: st}}
}

// names is what a command's arguments name, which says where it runs.
type names int

const (
	aRepository         names = iota // no issue: the repository it is run in, or --repo
	anIssue                          // its first argument is an issue
	anIssueOrNone                    // its first argument may be an issue: push
	everyRepository                  // no issue, and outside a repository every one, together: list, watch
	eachRepository                   // no issue, and outside a repository each one in turn: pull, ext list
	theWorkingDirectory              // the repository it is run in and no other: the migrations
)

// dispatch settles the repository a command runs on.
type dispatch struct {
	target *targetStore
	here   issuelib.Store // the repository the command is run in, if it is in one
	repo   string         // --repo, given before the command
	kinds  map[*cli.Command]names
	ws     *workspace
}

func newDispatch() *dispatch {
	d := &dispatch{here: issuelib.NewGitStore(), kinds: map[*cli.Command]names{}}
	d.target = &targetStore{Store: d.here, set: d.set}
	return d
}

// repoOpt is --repo, which the root takes and every command under it.
func (d *dispatch) repoOpt() *cli.Opt {
	return &cli.Opt{
		Name:        "repo",
		Description: "the repository to run on: its name in ~/.config/git-issue.tony, or its directory",
		Type: cli.NamedFuncOpt(cli.FuncOpt(func(cc *cli.Context, a string) (any, error) {
			d.repo = a
			return 0, nil
		}), "(name)"),
	}
}

// in makes cmd, and every command under it, run on the repository its
// arguments name.
func (d *dispatch) in(cmd *cli.Command, what names) *cli.Command {
	d.kinds[cmd] = what
	if run := cmd.Hooks.Run; run != nil {
		cmd.Hooks.Run = func(cc *cli.Context, args []string) error {
			// A command asked for its help answers anywhere. One that takes
			// options answers by parsing them; one that takes none would
			// read -h as its first argument, an issue, so it is answered for
			// here.
			for i, a := range args {
				switch a {
				case "-h", "-help", "--help":
					if len(cmd.Opts) == 0 && i == 0 {
						return cli.ErrUsage
					}
					return run(cc, args)
				}
			}
			args, name, err := cutRepo(args)
			if err != nil {
				return err
			}
			if name == "" {
				name = d.repo
			}
			if err := d.settle(cc, cmd, name, args); err != nil {
				return err
			}
			if len(d.target.each) > 0 {
				return d.inEach(cc, run, args)
			}
			return run(cc, args)
		}
	}
	for _, sub := range cmd.Children {
		d.in(sub, what)
	}
	return cmd
}

// inEach runs a command on each repository of the set in turn, saying which
// before what the command says. One that fails is said and the rest still
// run; the command fails if any did.
func (d *dispatch) inEach(cc *cli.Context, run cli.RunFunc, args []string) error {
	var failed []string
	for _, r := range d.target.each {
		d.on(r)
		fmt.Fprintf(cc.Out, "%s:\n", r.Name)
		if err := run(cc, args); err != nil {
			fmt.Fprintf(cc.Err, "%s: %v\n", r.Name, err)
			failed = append(failed, r.Name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed in %s", strings.Join(failed, ", "))
	}
	return nil
}

// sub says what one command under cmd names, when it is not what cmd's do.
func (d *dispatch) sub(cmd *cli.Command, name string, what names) *cli.Command {
	for _, c := range cmd.Children {
		if c.Name == name {
			d.kinds[c] = what
		}
	}
	return cmd
}

// cutRepo takes --repo and its value out of a command's arguments, wherever
// before a -- they are: a command that parses no options takes it too.
func cutRepo(args []string) (rest []string, name string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(rest, args[i:]...), name, nil
		}
		opt := strings.TrimPrefix(strings.TrimPrefix(a, "-"), "-")
		switch {
		case a == opt:
			rest = append(rest, a)
		case opt == "repo":
			if i+1 == len(args) {
				return nil, "", fmt.Errorf("%w: --repo needs a repository's name", cli.ErrUsage)
			}
			i++
			name = args[i]
		case strings.HasPrefix(opt, "repo="):
			name = strings.TrimPrefix(opt, "repo=")
		default:
			rest = append(rest, a)
		}
	}
	return rest, name, nil
}

// set reads the working set, once: the configured repositories, and the one
// the command is run in.
func (d *dispatch) set() (*workspace, error) {
	if d.ws != nil {
		return d.ws, nil
	}
	ws := newWorkspace(os.Stdout)
	// As NewGitStore: what a command prints on stdout, what a read corrected
	// on stderr.
	ws.open = func(dir string) issuelib.Store { return issuelib.NewGitStoreIn(dir) }
	configured, err := readWorkingSet()
	if err != nil {
		return nil, err
	}
	if root := repositoryRoot(""); root != "" {
		if _, err := ws.add(root, "cwd"); err != nil {
			return nil, err
		}
	}
	for _, dir := range configured {
		if _, err := ws.add(dir, "config"); err != nil {
			return nil, err
		}
	}
	d.ws = ws
	return ws, nil
}

// repositoryRoot answers the top of the repository dir is in, or dir's own
// git directory's place for one with no working tree; empty outside one. It
// is what names a repository in a set, so that one run from a directory
// inside it is the repository the set already has.
func repositoryRoot(dir string) string {
	if dir != "" {
		if at, err := os.Stat(dir); err != nil || !at.IsDir() {
			return ""
		}
	}
	for _, args := range [][]string{
		{"rev-parse", "--show-toplevel"},
		{"rev-parse", "--absolute-git-dir"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.Output(); err == nil && strings.TrimSpace(string(out)) != "" {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

// on points the commands' store at a repository of the set.
func (d *dispatch) on(r *repo) {
	d.target.Store, d.target.repo = r.Store, r
}

// settle points the commands' store at the repository cmd runs on, given its
// arguments and --repo.
func (d *dispatch) settle(cc *cli.Context, cmd *cli.Command, name string, args []string) error {
	what := d.kinds[cmd]
	inRepo := d.here.VerifyRepository() == nil
	if what == theWorkingDirectory {
		if name != "" {
			return fmt.Errorf("%w: %s runs on the repository it is run in; --repo does not go with it", cli.ErrUsage, cmd.Name)
		}
		return d.here.VerifyRepository()
	}
	if name != "" {
		ws, err := d.set()
		if err != nil {
			return err
		}
		// A name of the set's first; then a directory, which is a repository
		// whether the set has it or not, as it is to git -C.
		r, err := ws.byName(name)
		if err != nil {
			root := repositoryRoot(name)
			if root == "" {
				return fmt.Errorf("no repository named %q: one of %s", name, ws.names())
			}
			if r, err = ws.add(root, "--repo"); err != nil {
				return err
			}
		}
		d.on(r)
		return nil
	}
	if what == everyRepository {
		if ids := positional(cc, cmd, args); len(ids) > 0 {
			return d.holding(cc, ids, inRepo)
		}
	}
	if id := issueNamed(cc, cmd, what, args); id != "" {
		// Here first, and what is wrong with the id here -- it names several
		// -- is the command's to say, as it was.
		if inRepo {
			if _, err := d.here.FindRef(id); err == nil || strings.Contains(err.Error(), "ambiguous") {
				return nil
			}
		}
		ws, err := d.set()
		if err != nil {
			return err
		}
		if len(ws.list()) == 0 {
			return d.here.VerifyRepository()
		}
		r, _, err := ws.find(id)
		if err != nil {
			// A push's first argument may be a remote: no issue is not an
			// error of its, and it is a push of every issue.
			if what == anIssueOrNone {
				if !inRepo {
					d.target.each = ws.list()
				}
				return nil
			}
			return err
		}
		d.on(r)
		// The repository was not named, and is not the one the command is
		// run in: say which it is, beside what the command says.
		fmt.Fprintf(cc.Err, "in %s (%s)\n", r.Name, r.Dir)
		return nil
	}
	if inRepo {
		return nil
	}
	ws, err := d.set()
	if err != nil {
		return err
	}
	repos := ws.list()
	switch {
	case len(repos) == 0:
		return d.here.VerifyRepository()
	case what == anIssue:
		// No issue given: the command says how it is used.
		return nil
	case what == everyRepository:
		d.on(repos[0])
		d.target.every = repos
	case len(repos) == 1:
		d.on(repos[0])
		fmt.Fprintf(cc.Err, "in %s (%s)\n", repos[0].Name, repos[0].Dir)
	case what == eachRepository || what == anIssueOrNone:
		d.target.each = repos
	default:
		return fmt.Errorf("not in a repository: --repo says which, one of %s", ws.names())
	}
	return nil
}

// holding makes a command over every repository one over the repositories
// that hold the issues it is given: watch, given issues. Each is looked for
// in the repository the command is run in first, then in the set.
func (d *dispatch) holding(cc *cli.Context, ids []string, inRepo bool) error {
	var every []*repo
	add := func(r *repo) {
		for _, have := range every {
			if have == r {
				return
			}
		}
		every = append(every, r)
	}
	// The repository the command is run in, named as the rest are, by its
	// directory.
	var here *repo
	if inRepo {
		root := repositoryRoot("")
		here = &repo{Name: filepath.Base(root), Dir: root, Store: d.here}
	}
	elsewhere := false
	for _, id := range ids {
		if inRepo {
			if _, err := d.here.FindRef(id); err == nil || strings.Contains(err.Error(), "ambiguous") {
				add(here)
				continue
			}
		}
		ws, err := d.set()
		if err != nil {
			return err
		}
		if len(ws.list()) == 0 {
			return d.here.VerifyRepository()
		}
		r, _, err := ws.find(id)
		if err != nil {
			return err
		}
		fmt.Fprintf(cc.Err, "%s in %s (%s)\n", id, r.Name, r.Dir)
		add(r)
		elsewhere = true
	}
	if !elsewhere {
		return nil // all here: the command runs here, as it did
	}
	d.on(every[0])
	d.target.every = every
	return nil
}

// positional answers a command's arguments that are no option. A command
// that takes options parses them to find what is left; one that takes none
// is given its arguments as they are, a comment that begins with a dash
// among them.
func positional(cc *cli.Context, cmd *cli.Command, args []string) []string {
	if len(cmd.Opts) > 0 {
		parsed, err := cmd.Parse(cc, args)
		if err != nil {
			return nil // the command says what is wrong with them
		}
		args = parsed
	}
	var out []string
	for _, a := range args {
		if a != "--" {
			out = append(out, a)
		}
	}
	return out
}

// issueNamed answers the issue a command's arguments name: its first
// argument that is no option, when the command is given an issue.
func issueNamed(cc *cli.Context, cmd *cli.Command, what names, args []string) string {
	if what != anIssue && what != anIssueOrNone {
		return ""
	}
	if pos := positional(cc, cmd, args); len(pos) > 0 {
		return pos[0]
	}
	return ""
}

// mirrorFar brings the issue other into the repository from is in, when it
// is in another repository of the set: a relation to it then resolves from
// that repository alone. It answers other's id in full, and the repository
// it was mirrored from, none when it was here already.
func mirrorFar(ws *workspace, from *repo, other string) (id, mirrored string, err error) {
	to, id, err := ws.find(other)
	if err != nil {
		return "", "", err
	}
	if to == from {
		return id, "", nil
	}
	// The source is named for the other repository, and is its directory: the
	// fetch is local. A source of that name the repository already has is
	// left as it is, and is where the mirror is fetched from: where a
	// repository is, is for whoever recorded it to say.
	srcs, err := from.Store.Sources()
	if err != nil {
		return "", "", err
	}
	have := false
	for _, src := range srcs {
		have = have || src.Name == to.Name
	}
	if !have {
		if err := ops.SourceAdd(from.Store, to.Name, to.Dir); err != nil {
			return "", "", err
		}
	}
	if _, err := ops.Mirror(from.Store, to.Name, id); err != nil {
		return "", "", err
	}
	return id, to.Name, nil
}

// far is mirrorFar for a command: the issue a relation names, brought into
// the repository the command runs on when the set holds it elsewhere. An
// issue here already, or a store that is on no set, is left as it is.
func far(st issuelib.Store, other string) (mirrored string, err error) {
	t, ok := st.(*targetStore)
	if !ok {
		return "", nil
	}
	if _, err := t.Store.FindRef(other); err == nil || strings.Contains(err.Error(), "ambiguous") {
		return "", nil
	}
	ws, err := t.set()
	if err != nil {
		return "", err
	}
	from := t.repo
	if from == nil {
		// The repository the command is run in: the set has it by its root.
		root := repositoryRoot("")
		for _, r := range ws.list() {
			if r.Dir == root {
				from = r
			}
		}
	}
	if from == nil {
		return "", errors.New("not in a repository")
	}
	_, mirrored, err = mirrorFar(ws, from, other)
	return mirrored, err
}
