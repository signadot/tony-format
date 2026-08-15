package server

import (
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// TestScopedDeltaOnACommentOnlyChange holds the site to the policy, not just the
// policy to itself: emitScopedDeltaFrom is where a scoped watch decides whether
// a commit changed anything, and a comment-only change has to reach the watcher
// with a delta that carries the comment.
//
// Nothing stored carries a comment today, so this exercises the decision rather
// than a live path -- which is the point. It is the test that fails on the day
// someone reaches for the blind equality again (3cdjz00jh12krns4g1n0, section 4).
func TestScopedDeltaOnACommentOnlyChange(t *testing.T) {
	prev, err := parse.Parse([]byte("name: svc\n"), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	next, err := parse.Parse([]byte("# now with a reason\nname: svc\n"), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}

	s := NewSession("test", newMockConn(), &SessionConfig{})
	id := "w1"
	got, err := s.emitScopedDeltaFrom(&id, "", 7, prev, next)
	if err != nil {
		t.Fatal(err)
	}
	if got != next {
		t.Error("the watch's idea of the current document did not advance")
	}

	var resp *api.SessionResponse
	select {
	case resp = <-s.outgoing:
	default:
		t.Fatal("a comment-only change sent no event: the store would hold a commit every watch dropped")
	}
	if resp.Event == nil || resp.Event.Patch == nil {
		t.Fatalf("the event carries no patch: %+v", resp)
	}
	patched, err := tony.Patch(prev, resp.Event.Patch, mergeop.Comments(true))
	if err != nil {
		t.Fatal(err)
	}
	if !api.SameState(patched, next) {
		t.Errorf("the delta does not carry the change it announced: applying it gives %v", patched)
	}
}
