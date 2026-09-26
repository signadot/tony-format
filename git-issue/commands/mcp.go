package commands

import (
	"context"
	"fmt"
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
// ["issue", "mcp"]}}}. The repository is the process's working directory, as it
// is for every other subcommand -- a host starts a project-scoped server in the
// project -- and -C is for a host that does not. There is no listener and no
// address: serve binds loopback because nothing it serves authenticates, and
// stdio has no such question, the host owns both ends.
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
	Dir   string `cli:"name=C desc='run in this repository rather than the working directory'"`
}

// MCPCommand returns the mcp subcommand.
func MCPCommand(store issuelib.Store) *cli.Command {
	cfg := &mcpConfig{store: store}
	opts, _ := cli.StructOpts(cfg)
	return cli.NewCommandAt(&cfg.Command, "mcp").
		WithSynopsis("mcp [-C <dir>] - Serve the tracker to an agent's host over MCP on stdin/stdout").
		WithOpts(opts...).
		WithRun(cfg.run)
}

func (cfg *mcpConfig) run(cc *cli.Context, args []string) error {
	if _, err := cfg.Parse(cc, args); err != nil {
		return err
	}
	if cfg.Dir != "" {
		// GitStore acts on the process's working directory, so this is how a
		// repository is chosen.
		if err := os.Chdir(cfg.Dir); err != nil {
			return fmt.Errorf("cannot run in %s: %w", cfg.Dir, err)
		}
	}
	ctx := cc.Go
	if ctx == nil {
		ctx = context.Background()
	}
	// Nothing of the store's may reach stdout: it is the protocol's. Its
	// warnings go where a person reads them.
	store := issuelib.NewGitStoreWithOutput(os.Stderr)
	return MCPServer(store).Run(ctx, &mcp.StdioTransport{})
}

// MCPServer is the tracker as an MCP server over store, with every tool
// registered. A test connects one over an in-memory transport.
func MCPServer(store issuelib.Store) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "git-issue",
		Title:   "git-issue",
		Version: buildinfo.Version(),
	}, &mcp.ServerOptions{Instructions: mcpInstructions})
	addTools(s, store)
	return s
}

// mcpInstructions is what the host gives its model about this server: the
// tracker's rules, stated once, where the operations they govern are.
const mcpInstructions = `git-issue keeps this repository's issues in the repository itself, as git refs, so
they clone, branch and work offline with the code. An issue id is a 20-character XIDR; every tool
takes any unambiguous prefix of one, and answers with the full id.

The rules of the tracker:
  - Every change to the code gets an issue, filed before the work, and the commit that makes the
    change carries "Issue: <full id>" as a trailer.
  - A fix closes its issue with the commit that made it: issue_close with commit.
  - What an issue says is edited in place (issue_edit); what was decided is recorded as a comment
    (issue_comment). Both are commits on the issue's chain, so history keeps what it said before.
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
