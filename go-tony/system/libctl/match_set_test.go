package libctl

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// seedJobs writes n jobs and answers a session on the store.
func seedJobs(t *testing.T, s *LogdSession, ctx context.Context, n int) {
	t.Helper()
	jobs := map[string]*ir.Node{}
	for i := range n {
		name := string(rune('a' + i))
		status := "ready"
		if i%2 == 0 {
			status = "done"
		}
		jobs[name] = ir.FromKeyVals([]ir.KeyVal{
			{Key: ir.FromString("status"), Val: ir.FromString(status)},
			{Key: ir.FromString("n"), Val: ir.FromInt(int64(i))},
		})
	}
	if _, err := s.Patch(ctx, "", ir.FromKeyVals([]ir.KeyVal{
		{Key: ir.FromString("jobs"), Val: ir.FromMap(jobs)},
	})); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// TestMatchEach_ReadsASetOneNodeAtATime is the client end of y2agz9dyh12kse24n9n0: a
// wildcard path answers a set, the members arrive one at a time with their own paths,
// and the paging the server does is followed here rather than by the caller.
func TestMatchEach_ReadsASetOneNodeAtATime(t *testing.T) {
	srv := startLogd(t)
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "sets"})
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// One page's worth; the set that spans pages is the next test.
	seedJobs(t, s, ctx, 12)

	members, commit, err := s.MatchSet(ctx, "jobs.*", nil)
	if err != nil {
		t.Fatalf("MatchSet: %v", err)
	}
	if len(members) != 12 {
		t.Fatalf("the set has %d members, want 12", len(members))
	}
	if commit == 0 {
		t.Error("the set was read at no commit")
	}
	for _, m := range members {
		if m.Path == "" || m.Node == nil {
			t.Fatalf("member %+v", m)
		}
		// Every member path is one this session can read on its own.
		node, err := s.Match(ctx, m.Path)
		if err != nil {
			t.Fatalf("a member path is not readable on its own: %s: %v", m.Path, err)
		}
		if node == nil {
			t.Errorf("%s read back as nothing", m.Path)
		}
	}

	// A pattern selects and trims each member: the jobs that are done, just their n.
	done, _, err := s.MatchSet(ctx, "jobs.*", ir.FromKeyVals([]ir.KeyVal{
		{Key: ir.FromString("status"), Val: ir.FromString("done")},
	}))
	if err != nil {
		t.Fatalf("MatchSet with a pattern: %v", err)
	}
	if len(done) != 6 {
		t.Errorf("the pattern selected %d members, want 6", len(done))
	}
}

// TestMatchEach_FollowsPagesAcrossTheServersCap: a set larger than one page comes back
// whole, because MatchEach follows the cursor the marker carries. If it did not, the
// answer would stop at the server's own page size.
func TestMatchEach_FollowsPagesAcrossTheServersCap(t *testing.T) {
	srv := startLogd(t)
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "pages"})
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const n = 1200 // more than one page holds
	jobs := map[string]*ir.Node{}
	for i := range n {
		jobs[fmt.Sprintf("j%04d", i)] = ir.FromInt(int64(i))
	}
	if _, err := s.Patch(ctx, "", ir.FromKeyVals([]ir.KeyVal{
		{Key: ir.FromString("many"), Val: ir.FromMap(jobs)},
	})); err != nil {
		t.Fatalf("seed: %v", err)
	}

	seen := map[string]bool{}
	_, err := s.MatchEach(ctx, "many.*", nil, func(m SetMember) error {
		if seen[m.Path] {
			t.Errorf("%s was answered twice", m.Path)
		}
		seen[m.Path] = true
		return nil
	})
	if err != nil {
		t.Fatalf("MatchEach: %v", err)
	}
	if len(seen) != n {
		t.Errorf("the set answered %d members, want %d: the pages were not followed", len(seen), n)
	}
}

// TestMatchPaths_AnswersWhereWithoutWhat: the retspec end to end. Paths come back
// without bodies, a pattern still selects, and each path reads on its own.
func TestMatchPaths_AnswersWhereWithoutWhat(t *testing.T) {
	srv := startLogd(t)
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "paths"})
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	seedJobs(t, s, ctx, 6)

	paths, commit, err := s.MatchPaths(ctx, "jobs.*", nil)
	if err != nil {
		t.Fatalf("MatchPaths: %v", err)
	}
	if len(paths) != 6 {
		t.Fatalf("answered %d paths, want 6: %v", len(paths), paths)
	}
	if commit == 0 {
		t.Error("the set was read at no commit")
	}
	for _, p := range paths {
		if _, err := s.Match(ctx, p); err != nil {
			t.Errorf("a path answered by MatchPaths does not read: %s: %v", p, err)
		}
	}

	done, _, err := s.MatchPaths(ctx, "jobs.*", ir.FromKeyVals([]ir.KeyVal{
		{Key: ir.FromString("status"), Val: ir.FromString("done")},
	}))
	if err != nil {
		t.Fatalf("MatchPaths with a pattern: %v", err)
	}
	if len(done) != 3 {
		t.Errorf("the pattern selected %d paths, want 3: %v", len(done), done)
	}

	// The same listing by the name each member lives under, without the prefix the
	// caller already knows.
	ids, _, err := s.MatchIDs(ctx, "jobs.*", nil)
	if err != nil {
		t.Fatalf("MatchIDs: %v", err)
	}
	if want := []string{"a", "b", "c", "d", "e", "f"}; !equalStrings(ids, want) {
		t.Errorf("MatchIDs answered %v, want %v", ids, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestMatchEach_StopsEarly: a caller that has seen enough returns an error from fn, and
// the session is usable afterwards -- the members still on their way are not delivered
// to a request nobody is reading.
func TestMatchEach_StopsEarly(t *testing.T) {
	srv := startLogd(t)
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "stop"})
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	seedJobs(t, s, ctx, 8)

	errEnough := errors.New("enough")
	seen := 0
	_, err := s.MatchEach(ctx, "jobs.*", nil, func(m SetMember) error {
		seen++
		if seen == 2 {
			return errEnough
		}
		return nil
	})
	if !errors.Is(err, errEnough) {
		t.Fatalf("MatchEach = %v, want the caller's own error", err)
	}
	if seen != 2 {
		t.Errorf("fn saw %d members after stopping at 2", seen)
	}
	// The session still answers, which is what stopping early must not cost.
	if _, err := s.Match(ctx, "jobs.a"); err != nil {
		t.Fatalf("the session is unusable after stopping early: %v", err)
	}
}

// TestMatchEach_SinglePathStillAnswers: MatchEach over a path that names one node
// answers that one node, so a caller need not know which kind of path it holds.
func TestMatchEach_SinglePathStillAnswers(t *testing.T) {
	srv := startLogd(t)
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "one"})
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	seedJobs(t, s, ctx, 2)

	members, _, err := s.MatchSet(ctx, "jobs.a", nil)
	if err != nil {
		t.Fatalf("MatchSet at a single path: %v", err)
	}
	if len(members) != 1 || members[0].Path != "jobs.a" {
		t.Fatalf("answered %+v, want the one node at jobs.a", members)
	}

	// An answer carrying the path or the id is still the one answer, not a member of a
	// set with a marker to come (d4n7swjph12ksvxsn9n0).
	one, cancelOne := context.WithTimeout(ctx, 2*time.Second)
	defer cancelOne()
	if paths, _, err := s.MatchPaths(one, "jobs.a", nil); err != nil || !equalStrings(paths, []string{"jobs.a"}) {
		t.Errorf("MatchPaths at a single path answered %v, %v", paths, err)
	}
	if ids, _, err := s.MatchIDs(one, "jobs.a", nil); err != nil || !equalStrings(ids, []string{"a"}) {
		t.Errorf("MatchIDs at a single path answered %v, %v", ids, err)
	}
}

// TestMatchSet_ThroughDocd_IsUnsupported: docd routes by a path's field prefix, so a
// set spanning mounts has no single owner. Until docd composes one it says so, rather
// than forwarding the whole set to one participant and answering a different question
// (5f6vrzw0h12ksrtfn9n0).
func TestMatchSet_ThroughDocd_IsUnsupported(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdProxy(t, logd.TCPAddr())

	s := NewLogdSession(&LogdSessionConfig{Addr: docd.ClientTCPAddr(), ClientID: "via-docd"})
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	seedJobs(t, s, ctx, 3)

	_, _, err := s.MatchSet(ctx, "jobs.*", nil)
	if err == nil {
		t.Fatal("docd answered a set it cannot compose")
	}
	if code := logdapi.ErrorCode(err); code != logdapi.ErrCodeUnsupported {
		t.Errorf("code %q, want %q: %v", code, logdapi.ErrCodeUnsupported, err)
	}
	// A path that names one node still reads through docd, unchanged.
	if _, err := s.Match(ctx, "jobs.a"); err != nil {
		t.Errorf("a single-node read through docd: %v", err)
	}
}

// TestMatchEachReturning_IterType is the client end of 1k6w71sfh12ksn5qn9n0: a member
// says what kind of node it is, and the wildcard for that kind appended to its path is
// the set under it -- the walk a client browsing the store makes, one level at a time.
func TestMatchEachReturning_IterType(t *testing.T) {
	srv := startLogd(t)
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "kinds"})
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	seedJobs(t, s, ctx, 3)

	ret := logdapi.ReturnPath + "," + logdapi.ReturnIterType
	var jobs []SetMember
	if _, err := s.MatchEachReturning(ctx, "jobs.*", nil, ret, func(m SetMember) error {
		jobs = append(jobs, m)
		return nil
	}); err != nil {
		t.Fatalf("MatchEachReturning: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("answered %d jobs, want 3", len(jobs))
	}
	for _, job := range jobs {
		if job.IterType != logdapi.IterObject || job.Node != nil {
			t.Fatalf("%s: iterType %q, node %v; want an Object and no body", job.Path, job.IterType, job.Node)
		}
		var fields []SetMember
		if _, err := s.MatchEachReturning(ctx, job.Path+logdapi.IterWildcard(job.IterType), nil, ret, func(m SetMember) error {
			fields = append(fields, m)
			return nil
		}); err != nil {
			t.Fatalf("listing %s: %v", job.Path, err)
		}
		kinds := map[string]string{}
		for _, f := range fields {
			kinds[f.Path] = f.IterType
		}
		if kinds[job.Path+".status"] != logdapi.IterString || kinds[job.Path+".n"] != logdapi.IterNumber || len(kinds) != 2 {
			t.Errorf("listing %s answered %v", job.Path, kinds)
		}
	}
}

// TestReturn_ThroughDocd: a read docd passes through to logd answers its retspec as logd
// does. Past a mount it cannot -- a controller answers a body, and a composed read is a
// body docd assembles -- so a retspec asking for more than the body is unsupported there,
// said rather than answered with the body alone (1k6w71sfh12ksn5qn9n0).
func TestReturn_ThroughDocd(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	mem := newMemController()
	mem.data["a.b"] = vObj(7)
	runController(t, docd, "a.b", mem)

	client := docdClient(t, docd, "client")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Patch(ctx, "a.x", vObj(1)); err != nil {
		t.Fatalf("seed base: %v", err)
	}

	answer := func(path, ret string) ([]SetMember, error) {
		var out []SetMember
		_, err := client.MatchEachReturning(ctx, path, nil, ret, func(m SetMember) error {
			out = append(out, m)
			return nil
		})
		return out, err
	}

	// Passed through: logd's answer, whole.
	got, err := answer("a.x", "path,iterType")
	if err != nil {
		t.Fatalf("pass-through: %v", err)
	}
	if len(got) != 1 || got[0].Path != "a.x" || got[0].IterType != logdapi.IterObject || got[0].Node != nil {
		t.Errorf("pass-through answered %+v", got)
	}

	for _, tc := range []struct{ name, path, ret string }{
		{"composed", "a", "iterType"},
		{"composed path", "a", "path,body"},
		{"at the mount", "a.b", "path"},
		{"under the mount", "a.b.v", "iterType"},
	} {
		if _, err := answer(tc.path, tc.ret); logdapi.ErrorCode(err) != logdapi.ErrCodeUnsupported {
			t.Errorf("%s: return %q at %s answered %v, want %s", tc.name, tc.ret, tc.path, err, logdapi.ErrCodeUnsupported)
		}
	}

	// The body is what both can answer, asked for by name or by default.
	for _, path := range []string{"a", "a.b"} {
		for _, ret := range []string{"", "body"} {
			if got, err := answer(path, ret); err != nil || len(got) != 1 || got[0].Node == nil {
				t.Errorf("return %q at %s answered %+v, %v", ret, path, got, err)
			}
		}
	}
}
