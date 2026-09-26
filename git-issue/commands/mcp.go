package commands

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/go-tony/buildinfo"
)

// `git issue mcp` is the tracker as an MCP server: what an agent's host starts,
// speaks to over stdin and stdout, and gates tool by tool. It is the second
// front end over ops, beside the commands -- each tool parses its typed
// arguments, calls the same function the command calls, and answers what it did
// as structured content, with the text a person would have seen alongside.
//
// It is for a host to start, not a person:
//
//	claude mcp add git-issue -- git issue mcp
//
// or, in .mcp.json, {"mcpServers": {"git-issue": {"command": "git", "args":
// ["issue", "mcp"]}}}. There is no listener and no address: serve binds
// loopback because nothing it serves authenticates, and stdio has no such
// question, the host owns both ends.
//
// The repositories it serves are its working set (mcp_workspace.go), and come
// from the first of: -C, repeatable; ~/.config/git-issue.tony; the repository
// the server was started in. A server started outside any repository with no
// -C and no config refuses and says so. repo_add and repo_remove change the set
// while it runs. One repository served is what it was: no tool needs a repo.
//
// Push and pull are tools of their own, not options of one, so a host that
// wants an agent working locally and never touching the remote denies two
// names. A refusal the store makes -- an unknown id, a closed issue closed
// again, a merge it cannot make -- is a tool error carrying the store's
// message, never a protocol error: the agent reads why, as a person reads it
// from the command.

type mcpConfig struct {
	*cli.Command
	store issuelib.Store
	Dirs  []string
}

// MCPCommand returns the mcp subcommand.
func MCPCommand(store issuelib.Store) *cli.Command {
	cfg := &mcpConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "mcp").
		WithSynopsis("mcp [-C <dir>]... - Serve the tracker to an agent's host over MCP on stdin/stdout").
		WithOpts(&cli.Opt{
			Name:        "C",
			Description: "a repository to serve; repeatable. With none, ~/.config/git-issue.tony, else the working directory",
			Type: cli.NamedFuncOpt(cli.FuncOpt(func(cc *cli.Context, a string) (any, error) {
				cfg.Dirs = append(cfg.Dirs, a)
				return 0, nil
			}), "(dir)"),
		}).
		WithRun(cfg.run)
}

func (cfg *mcpConfig) run(cc *cli.Context, args []string) error {
	if _, err := cfg.Parse(cc, args); err != nil {
		return err
	}
	ctx := cc.Go
	if ctx == nil {
		ctx = context.Background()
	}
	// Nothing of the stores' may reach stdout: it is the protocol's. Their
	// warnings go where a person reads them.
	ws, err := startingSet(cfg.Dirs, os.Stderr)
	if err != nil {
		return err
	}
	return MCPServerFor(ws).Run(ctx, &mcp.StdioTransport{})
}

// startingSet is the working set a server starts with: the -C list, else the
// configured set, else the working directory when it is a repository.
func startingSet(dirs []string, out io.Writer) (*workspace, error) {
	ws := newWorkspace(out)
	from := "-C"
	if len(dirs) == 0 {
		configured, err := readWorkingSet()
		if err != nil {
			return nil, err
		}
		dirs, from = configured, "config"
	}
	if len(dirs) == 0 {
		if err := issuelib.NewGitStoreAt("", out).VerifyRepository(); err != nil {
			return nil, errors.New("not in a repository, no -C given and no ~/.config/git-issue.tony: nothing to serve")
		}
		dirs, from = []string{"."}, "cwd"
	}
	for _, dir := range dirs {
		if _, err := ws.add(dir, from); err != nil {
			return nil, err
		}
	}
	return ws, nil
}

// MCPServer is the tracker as an MCP server over one store, with every tool
// registered: the shape a test connects to over an in-memory transport.
func MCPServer(store issuelib.Store) *mcp.Server {
	ws := newWorkspace(io.Discard)
	dir := ""
	if gs, ok := store.(*issuelib.GitStore); ok {
		dir = gs.Dir()
	}
	name := "repo"
	if dir != "" {
		name = dir
	}
	ws.repos = append(ws.repos, &repo{Name: name, Dir: dir, From: "store", Store: store})
	return MCPServerFor(ws)
}

// MCPServerFor is the tracker as an MCP server over a working set.
func MCPServerFor(ws *workspace) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "git-issue",
		Title:   "git-issue",
		Version: buildinfo.Version(),
	}, &mcp.ServerOptions{Instructions: mcpInstructions})
	addTools(s, ws)
	return s
}

// mcpInstructions is what the host gives its model about this server: the
// tracker's rules, stated once, where the operations they govern are.
const mcpInstructions = `git-issue keeps a repository's issues in the repository itself, as git refs, so
they clone, branch and work offline with the code. An issue id is a 20-character XIDR, unique
across repositories; every tool takes any unambiguous prefix of one, and answers with the full id.

The server serves a working set of repositories (repo_list; repo_add and repo_remove change it).
A tool given an id finds the repository that holds it. issue_create, issue_list, issue_push and
issue_pull take repo when more than one is served.

The rules of the tracker:
  - Every change to the code gets an issue, filed before the work, and the commit that makes the
    change carries "Issue: <full id>" as a trailer.
  - A fix closes its issue with the commit that made it: issue_close with commit.
  - What an issue says is edited in place (issue_edit); what was decided is recorded as a comment
    (issue_comment). Both are commits on the issue's chain, so history keeps what it said before.
  - A relation across repositories mirrors the far issue into the near repository first, read-only
    there (an ext reference), so the relation resolves from that repository alone.
  - issue_push and issue_pull touch the remote and nothing else does. A push carries a lease on
    what the last fetch saw; an issue edited on both sides is refused and named, for a person to
    decide, never overwritten.`

// textOut renders through a command's writer, so a tool's text is what the
// command prints.
type textOut struct{ strings.Builder }

func (*textOut) Close() error { return nil }

func (t *textOut) cc() *cli.Context {
	return &cli.Context{Out: t, Err: nopWC{io.Discard}}
}

type nopWC struct{ io.Writer }

func (nopWC) Close() error { return nil }

// result is a tool's answer: the text a person would see, and the typed value
// the SDK carries as structured content.
func result(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.TrimRight(text, "\n")}}}
}
