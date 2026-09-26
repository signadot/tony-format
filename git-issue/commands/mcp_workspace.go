package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/parse"
)

// The working set: the repositories one server serves.
//
// The server is a view and holds nothing. It is N stores, each on its own
// repository's refs, and what it adds is dispatch: which repository holds an id
// is answered by searching every one of them, every time -- derived, never
// kept. The set itself is the user's configuration, the same kind of thing as
// remotes in .git/config, and lives nowhere in git because it need not survive
// anything: delete it and every issue is exactly where it was. Nothing the
// server knows is unrecoverable from the repositories (7qfhwth7h12ksxcwpxn0).
//
// An id names its repository without saying so -- an XIDR is unique across
// repositories -- which is what lets one server answer for several.

// repo is one repository the server serves.
type repo struct {
	Name  string // the directory's base name, or the full path when two collide
	Dir   string
	From  string // where it came from: -C, config, cwd, repo_add
	Store issuelib.Store
}

// workspace is the set, and the dispatch over it.
type workspace struct {
	mu    sync.Mutex
	repos []*repo
	out   io.Writer // where the stores' warnings go
}

func newWorkspace(out io.Writer) *workspace {
	return &workspace{out: out}
}

// add opens the repository at dir and serves it from then on. A directory that
// is not a repository is refused; one already served is answered as it is.
func (w *workspace) add(dir, from string) (*repo, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	store := issuelib.NewGitStoreAt(abs, w.out)
	if err := store.VerifyRepository(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, r := range w.repos {
		if r.Dir == abs {
			return r, nil
		}
	}
	name := filepath.Base(abs)
	for _, r := range w.repos {
		if r.Name == name {
			name = abs
			break
		}
	}
	r := &repo{Name: name, Dir: abs, From: from, Store: store}
	w.repos = append(w.repos, r)
	return r, nil
}

// remove stops serving a repository. Its issues are where they were.
func (w *workspace) remove(name string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, r := range w.repos {
		if r.Name == name || r.Dir == name {
			w.repos = append(w.repos[:i], w.repos[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("no repository named %q is served; repo_list says which are", name)
}

// list answers the repositories served, in the order they were added.
func (w *workspace) list() []*repo {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]*repo(nil), w.repos...)
}

// names is the repositories' names, for a message.
func (w *workspace) names() string {
	var names []string
	for _, r := range w.list() {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// byName answers the repository a tool was told, or the only one when it was
// told none and one is served.
func (w *workspace) byName(name string) (*repo, error) {
	repos := w.list()
	if len(repos) == 0 {
		return nil, errors.New("no repository is served; repo_add one")
	}
	if name == "" {
		if len(repos) == 1 {
			return repos[0], nil
		}
		return nil, fmt.Errorf("repo: which repository? one of %s", w.names())
	}
	for _, r := range repos {
		if r.Name == name || r.Dir == name {
			return r, nil
		}
	}
	return nil, fmt.Errorf("no repository named %q is served; one of %s", name, w.names())
}

// find answers the repository that holds an id and the id in full. An id found
// in none is not found; one a prefix reaches in more than one is ambiguous,
// naming each repository it matched in.
//
// One issue can be in two repositories: its own, and one that mirrors it as an
// ext reference. Its own wins -- that is where it is written -- and a mirror
// answers only when the repository that owns it is not served.
func (w *workspace) find(id string) (*repo, string, error) {
	repos := w.list()
	if len(repos) == 0 {
		return nil, "", errors.New("no repository is served; repo_add one")
	}
	type match struct {
		repo *repo
		xidr string
		ext  bool
	}
	var matches []match
	var ambiguous []string
	for _, r := range repos {
		ref, err := r.Store.FindRef(id)
		switch {
		case err == nil:
			xidr, err := issuelib.XIDRFromRef(ref)
			if err != nil {
				return nil, "", err
			}
			matches = append(matches, match{r, xidr, issuelib.IsExtRef(ref)})
		case strings.Contains(err.Error(), "ambiguous"):
			ambiguous = append(ambiguous, r.Name)
		}
	}
	if len(ambiguous) == 0 && len(matches) > 0 {
		one := true
		for _, m := range matches[1:] {
			if m.xidr != matches[0].xidr {
				one = false
			}
		}
		if one {
			for _, m := range matches {
				if !m.ext {
					return m.repo, m.xidr, nil
				}
			}
			return matches[0].repo, matches[0].xidr, nil
		}
	}
	if len(matches) == 0 && len(ambiguous) == 0 {
		return nil, "", fmt.Errorf("issue not found: %s (in %s)", id, w.names())
	}
	var where []string
	for _, m := range matches {
		where = append(where, m.xidr+" in "+m.repo.Name)
	}
	for _, name := range ambiguous {
		where = append(where, "several in "+name)
	}
	return nil, "", fmt.Errorf("ambiguous: %q matches %s", id, strings.Join(where, ", "))
}

// The working set on disk: ~/.config/git-issue.tony, a list of repositories a
// server started outside any of them serves. $XDG_CONFIG_HOME when set, and
// literally ~/.config otherwise -- not os.UserConfigDir, which on a Mac is
// ~/Library/Application Support and is not where anyone looks for this.
type workingSetConfig struct {
	Repos []string `tony:"field=repos"`
}

func configPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "git-issue.tony"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "git-issue.tony"), nil
}

// readWorkingSet answers the configured repositories, none when there is no
// file.
func readWorkingSet() ([]string, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	node, err := parse.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var cfg workingSetConfig
	if err := gomap.FromTonyIR(node, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg.Repos, nil
}

// persistWorkingSet adds a repository to the configured set, once.
func persistWorkingSet(dir string) error {
	repos, err := readWorkingSet()
	if err != nil {
		return err
	}
	for _, have := range repos {
		if have == dir {
			return nil
		}
	}
	repos = append(repos, dir)
	node, err := gomap.ToTonyIR(workingSetConfig{Repos: repos})
	if err != nil {
		return err
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(encode.MustString(node)), 0o644)
}
