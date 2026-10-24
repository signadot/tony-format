package issuelib

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/parse"
)

// Ext references: another repository's issue, mirrored into this one.
//
// A relation is a bare XIDR in meta.tony, and means something only where that id
// resolves. An issue here that is about an issue in another repository has no
// way to say so that a clone of this repository alone can follow -- unless the
// other issue is HERE. So it is fetched here, its whole chain, under
// ext/<source>/<xidr>, and from then on resolves like any issue of this
// repository: FindRef finds it, show reads it, a relation naming it is whole in
// every clone, because a clone carries every ref. The reference lives nowhere
// outside the repository, which is what keeps the distributed model whole.
//
// A mirror is read-only: it is the source's chain, byte for byte, and only the
// source's. A write to it is refused, naming where it lives, and a refresh is a
// fast-forward and never a merge, because nothing here ever adds to the chain.
//
// A source is a name this repository gives another, as a remote is named, and a
// ref sources/<source> whose tree holds source.tony: the URL, and when the
// mirrors from it were last fetched. The mirrors are refreshed by pull and
// carried by push, to this repository's origin and never to the source
// (sync.go) -- and the sources ref with them, so the URL it records is what
// every clone that pulls will fetch from. A directory on this machine is not
// that: given one, AddSource records the directory's origin as the URL and keeps
// the directory in this clone's git config, git-issue.source.<source>.path, as
// where this clone alone fetches from (p76fwh8gh12kr1ppvdn0).

// Source is where mirrors come from.
type Source struct {
	Name    string
	URL     string    // what the sources ref records: where every clone fetches from
	Path    string    // this clone's own way to it, from its git config; "" for none
	Fetched time.Time // when a mirror from it was last fetched; zero for never
}

// IsLocalPath says whether a URL is a path on a machine rather than a URL any
// clone could fetch from: a file URL, or no scheme and not host:path.
func IsLocalPath(url string) bool {
	if strings.HasPrefix(url, "file://") {
		return true
	}
	if strings.Contains(url, "://") {
		return false
	}
	i := strings.Index(url, ":")
	return i <= 0 || strings.Contains(url[:i], "/")
}

// localDir answers the directory a URL names on this machine, "" when it names
// none: a directory that exists, given as a path or as a file URL.
func localDir(url string) string {
	dir := strings.TrimPrefix(url, "file://")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	return abs
}

// sourcePathKey is the git config key holding this clone's own path to a source.
func sourcePathKey(name string) string { return "git-issue.source." + name + ".path" }

// originOf answers the origin URL of the repository whose root is dir, "" for
// none. A directory inside a repository is not that repository: git would find
// the enclosing one, and its origin is not where this directory's issues are.
func originOf(dir string) string {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return ""
	}
	top, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil || strings.TrimSpace(string(top)) != real {
		bare, err := exec.Command("git", "-C", dir, "rev-parse", "--is-bare-repository").Output()
		if err != nil || strings.TrimSpace(string(bare)) != "true" {
			return ""
		}
	}
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// sourceRecord is source.tony as stored.
type sourceRecord struct {
	URL     string `tony:"field=url"`
	Fetched string `tony:"field=fetched, optional"`
}

// AddSource records a source: a name for another repository and where it is.
// Naming a source again replaces its URL and keeps its mirrors.
//
// A directory on this machine, as a path or a file URL, is recorded as its
// origin, since the record is every clone's, and kept in this clone's config as
// where this clone fetches from. A directory with no origin is recorded as it
// is: there is nothing else to record, and the caller says that only this
// machine can reach it.
func (s *GitStore) AddSource(name, url string) error {
	if name == "" || strings.ContainsAny(name, "/ \t\n") || strings.HasPrefix(name, ".") {
		return fmt.Errorf("%q is not a source name: one word, no slash", name)
	}
	if url == "" {
		return fmt.Errorf("a source needs a URL or a path")
	}
	path := ""
	if dir := localDir(url); dir != "" {
		url = dir
		if origin := originOf(dir); origin != "" {
			url, path = origin, dir
		}
	}
	rec := sourceRecord{URL: url}
	if have, err := s.readSource(name); err == nil && !have.Fetched.IsZero() {
		rec.Fetched = have.Fetched.UTC().Format(time.RFC3339)
	}
	// A version of the source this clone fetched from a remote and has not
	// taken -- a sync refused to merge it -- is recorded over: whoever records
	// the source has said what it is, so the next sync carries this forward
	// rather than refusing it again, whichever URL was kept.
	tip := ""
	if held := s.refsAt(SourceRef(name)); len(held) == 1 {
		tip = held[0].commit
	}
	var over []string
	for _, r := range s.refsAt(nsRoot + "/remotes/*/sources/" + name) {
		if r.commit != tip && (tip == "" || !s.isAncestor(r.commit, tip)) {
			over = append(over, r.commit)
		}
	}
	if err := s.writeSource(name, rec, "source: "+name+" at "+url, over...); err != nil {
		return err
	}
	return s.configSet(sourcePathKey(name), path)
}

// Source answers the source of that name.
func (s *GitStore) Source(name string) (Source, error) { return s.readSource(name) }

// Sources answers every source this repository knows, by name.
func (s *GitStore) Sources() ([]Source, error) {
	var out []Source
	for _, r := range s.refsAt(SourcesPrefix + "*") {
		name := strings.TrimPrefix(r.ref, SourcesPrefix)
		src, err := s.readSource(name)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, nil
}

// Mirror fetches the issue xidr from source into ext/<source>/<xidr> and answers
// it as read here. The id is a full XIDR: a prefix cannot be resolved in a
// repository that is not here. An issue this repository holds as its own is
// refused, since a mirror of one's own issue is a second copy of it.
func (s *GitStore) Mirror(source, xidr string) (*Issue, error) {
	src, err := s.readSource(source)
	if err != nil {
		return nil, err
	}
	if len(xidr) != 20 {
		return nil, fmt.Errorf("a mirror names the issue by its full id, not %q", xidr)
	}
	if own := s.refsAt(RefForXIDR(xidr), ClosedRefForXIDR(xidr)); len(own) > 0 {
		return nil, fmt.Errorf("%s is this repository's own issue", xidr)
	}
	if err := s.fetchMirror(src, xidr); err != nil {
		return nil, err
	}
	if err := s.touchSource(source); err != nil {
		return nil, err
	}
	issue, _, err := s.GetByRef(ExtRefForXIDR(source, xidr))
	return issue, err
}

// RefreshMirrors brings every mirror from source up to what the source holds,
// and answers how many it looked at. A source that cannot be reached leaves its
// mirrors as they were, and that is the error.
func (s *GitStore) RefreshMirrors(source string) (int, error) {
	src, err := s.readSource(source)
	if err != nil {
		return 0, err
	}
	// The fetch is recorded when a mirror moved, and not otherwise: a refresh
	// that brought nothing has nothing to say, and one recorded every time is
	// a commit on the source's ref at every pull, which a watch makes every
	// few seconds (wnhrpxfeh12krs4cq5n0).
	n, moved := 0, false
	for _, r := range s.refsAt(ExtPrefix + source + "/*") {
		_, xidr, ok := ExtSource(r.ref)
		if !ok {
			continue
		}
		if err := s.fetchMirror(src, xidr); err != nil {
			return n, err
		}
		n++
		if at, err := s.GetRefCommit(r.ref); err != nil || at != r.commit {
			moved = true
		}
	}
	if moved {
		if err := s.touchSource(source); err != nil {
			return n, err
		}
	}
	return n, nil
}

// Unmirror removes the mirror at ref. What named it is the caller's to settle
// (ops.Unmirror strips the relations).
func (s *GitStore) Unmirror(ref string) error {
	if !IsExtRef(ref) {
		return fmt.Errorf("%s is not a mirror", ref)
	}
	held := s.refsAt(ref)
	if len(held) != 1 {
		return fmt.Errorf("no mirror at %s", ref)
	}
	return s.deleteRef(ref, held[0].commit)
}

// fetchMirror fetches one issue's chain from src into its mirror ref. The
// issue is open or closed there, and the source's namespace says which, so the
// open name is tried and then the closed one. The refspec is not forced: a
// mirror only ever moves forward along the source's chain, and a source that
// rewrote an issue's history is refused rather than followed. The fetch is from
// this clone's own path when it has one, and from the shared URL otherwise; a
// failure says which, so that a wrong one is found where it is written. The
// path stands for the URL, so one that is not a checkout of it -- the record
// moved on, or the directory did -- is refused rather than fetched from, which
// would follow a repository the record no longer names.
func (s *GitStore) fetchMirror(src Source, xidr string) error {
	dst := ExtRefForXIDR(src.Name, xidr)
	at, via := src.URL, "the url "+SourceRef(src.Name)+" records, which every clone fetches from"
	if src.Path != "" {
		if origin := originOf(src.Path); origin != src.URL {
			return fmt.Errorf("cannot fetch %s from %s: git config %s is %s, which is not a checkout of %s (the url %s records); `git issue ext add %s <path>` records the right one",
				xidr, src.Name, sourcePathKey(src.Name), src.Path, src.URL, SourceRef(src.Name), src.Name)
		}
		at, via = src.Path, "this clone's own path to it, git config "+sourcePathKey(src.Name)
	}
	var last string
	for _, from := range []string{RefForXIDR(xidr), ClosedRefForXIDR(xidr)} {
		// Not --quiet: a rejection (a mirror that moved off the source's chain,
		// which nothing here should do) is a line worth having.
		out, err := s.netOutput("fetch", at, from+":"+dst)
		if err == nil {
			return nil
		}
		last = strings.TrimSpace(string(out))
	}
	return fmt.Errorf("cannot fetch %s from %s at %s (%s): %s", xidr, src.Name, at, via, last)
}

// readSource reads source.tony at the source's ref, and this clone's own path
// to the source from its config.
func (s *GitStore) readSource(name string) (Source, error) {
	rec, fetched, err := s.sourceAt(SourceRef(name))
	switch {
	case errors.Is(err, errNoSourceRecord):
		return Source{}, fmt.Errorf("no source named %q: `git issue ext add %s <url|path>` records one", name, name)
	case err != nil:
		return Source{}, fmt.Errorf("source %s: %w", name, err)
	}
	return Source{Name: name, URL: rec.URL, Path: s.configGet(sourcePathKey(name)), Fetched: fetched}, nil
}

// errNoSourceRecord is a commit, or a ref, that holds no source.tony.
var errNoSourceRecord = errors.New("no source.tony")

// sourceAt reads source.tony as a ref or commit of a source's chain holds it,
// and when it says its mirrors were last fetched, zero for never.
func (s *GitStore) sourceAt(rev string) (sourceRecord, time.Time, error) {
	raw, err := s.ReadFile(rev, "source.tony")
	if err != nil {
		return sourceRecord{}, time.Time{}, errNoSourceRecord
	}
	node, err := parse.Parse(raw)
	if err != nil {
		return sourceRecord{}, time.Time{}, err
	}
	var rec sourceRecord
	if err := gomap.FromTonyIR(node, &rec); err != nil {
		return sourceRecord{}, time.Time{}, err
	}
	var fetched time.Time
	if rec.Fetched != "" {
		fetched, _ = time.Parse(time.RFC3339, rec.Fetched)
	}
	return rec, fetched, nil
}

// configGet answers a key of this repository's own config, "" when unset.
func (s *GitStore) configGet(key string) string {
	out, err := s.git("config", "--local", "--get", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// configSet sets a key of this repository's own config, or unsets it for "".
func (s *GitStore) configSet(key, value string) error {
	if value == "" {
		out, err := s.git("config", "--local", "--unset", key).CombinedOutput()
		if err != nil && !strings.Contains(err.Error(), "exit status 5") { // 5: it was not set
			return fmt.Errorf("git config --unset %s: %w: %s", key, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if out, err := s.git("config", "--local", key, value).CombinedOutput(); err != nil {
		return fmt.Errorf("git config %s: %w: %s", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// touchSource records that the source's mirrors were fetched now.
func (s *GitStore) touchSource(name string) error {
	src, err := s.readSource(name)
	if err != nil {
		return err
	}
	rec := sourceRecord{URL: src.URL, Fetched: time.Now().UTC().Format(time.RFC3339)}
	return s.writeSource(name, rec, "source: fetched from "+name)
}

// writeSource commits source.tony at the source's ref: a new root commit for a
// source not known yet, a commit on its chain otherwise, and a commit over the
// versions named in over too, when there are any.
func (s *GitStore) writeSource(name string, rec sourceRecord, message string, over ...string) error {
	ref := SourceRef(name)
	held := s.refsAt(ref)
	if len(held) == 1 && len(over) == 0 {
		node, err := gomap.ToTonyIR(rec)
		if err != nil {
			return err
		}
		return s.updateCommit(ref, message, map[string]string{"source.tony": encode.MustString(node)})
	}
	old, parents := zeroSHA, over
	if len(held) == 1 {
		old, parents = held[0].commit, append([]string{held[0].commit}, over...)
	}
	commit, err := s.commitSource(rec, message, parents...)
	if err != nil {
		return err
	}
	return s.setRef(ref, commit, old)
}

// commitSource writes a commit whose tree is source.tony holding rec, with the
// parents given, and answers it. It moves no ref.
func (s *GitStore) commitSource(rec sourceRecord, message string, parents ...string) (string, error) {
	node, err := gomap.ToTonyIR(rec)
	if err != nil {
		return "", err
	}
	hash := s.git("hash-object", "-w", "--stdin")
	hash.Stdin = strings.NewReader(encode.MustString(node))
	hashOut, err := hash.Output()
	if err != nil {
		return "", fmt.Errorf("failed to write source.tony: %w", err)
	}
	mktree := s.git("mktree")
	mktree.Stdin = strings.NewReader(fmt.Sprintf("100644 blob %s\tsource.tony\n", strings.TrimSpace(string(hashOut))))
	treeOut, err := mktree.Output()
	if err != nil {
		return "", fmt.Errorf("failed to write the source's tree: %w", err)
	}
	args := []string{"commit-tree", strings.TrimSpace(string(treeOut)), "-m", message}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	commit := s.git(args...)
	var stderr bytes.Buffer
	commit.Stderr = &stderr
	commitOut, err := commit.Output()
	if err != nil {
		return "", fmt.Errorf("failed to commit the source: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(commitOut)), nil
}
