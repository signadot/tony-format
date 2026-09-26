package commands

import (
	"fmt"
	"strings"
	"testing"

	"github.com/scott-cotton/cli"
)

// runRoot runs the command args name under Root, as the dispatcher would but
// for exiting the process.
func runRoot(cc *cli.Context, args []string) error {
	cmd := Root()
	for len(args) > 0 && cmd.Hooks.Run == nil {
		sub := cmd.FindSub(cc, args[0])
		if sub == nil {
			return fmt.Errorf("no command %q", args[0])
		}
		cmd, args = sub, args[1:]
	}
	return cmd.Run(cc, args)
}

// TestRoot_OutsideARepository: a command that needs a repository says there
// is none, whatever it would have failed on first; help, version and the
// commands run inside one are as they were (eahqxymwh12ks32vq1n0).
func TestRoot_OutsideARepository(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, args := range [][]string{
		{"create", "t", "--body", "b"},
		{"show", "abcd"},
		{"ext", "add", "s", "https://example.com/x"},
		{"ext", "list"},
		{"ext", "refresh"},
		{"push", "--all"},
		{"pull"},
		{"for-commit", "HEAD"},
		{"serve"},
	} {
		cc, _ := sayCC()
		err := runRoot(cc, args)
		if err == nil || !strings.Contains(err.Error(), "not a git repository") {
			t.Errorf("%v: %v, want not a git repository", args, err)
		}
	}
	cc, out := sayCC()
	if err := runRoot(cc, []string{"version"}); err != nil || out.Len() == 0 {
		t.Errorf("version outside a repository: %v, %q", err, out.String())
	}
	cc, _ = sayCC()
	if err := runRoot(cc, []string{"create", "-h"}); err != nil && strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("create -h outside a repository: %v", err)
	}

	testRepo(t) // chdirs into a repository
	cc, out = sayCC()
	if err := runRoot(cc, []string{"create", "In one", "--body", "b"}); err != nil || !strings.Contains(out.String(), "Created issue") {
		t.Errorf("create inside a repository: %v, %q", err, out.String())
	}
}
