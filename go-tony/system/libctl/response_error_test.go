package libctl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// The tests here pin ONE property: the code the server put on a response error is
// still reachable after libctl has handed the error to a caller. They are written
// against the code and never against the message, since a message that can be
// reworded is exactly what the code exists to replace.

// TestMatchAbsentPathCarriesNotFound is the regression test for the whole point:
// reading a path that holds nothing must be distinguishable from failing to read.
// While libctl formatted resp.Error.Message and dropped the code, the only way to
// tell was to match the prose — so when logd's PathError rewrote "path not found"
// into "no value at ...", callers matching the old spelling classified every absent
// read as a failure.
func TestMatchAbsentPathCarriesNotFound(t *testing.T) {
	srv := startLogd(t)
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "codes"})
	defer s.Close()
	ctx := context.Background()

	// Seed something, so the store is healthy and the read below fails for the one
	// reason under test rather than for want of a document.
	if _, err := s.Patch(ctx, "users/1", ir.FromMap(map[string]*ir.Node{
		"name": ir.FromString("test"),
	})); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := s.Match(ctx, "users/2")
	if err == nil {
		t.Fatal("expected an error reading an absent path")
	}
	if got := logdapi.ErrorCode(err); got != logdapi.ErrCodeNotFound {
		t.Errorf("ErrorCode = %q, want %q (err: %v)", got, logdapi.ErrCodeNotFound, err)
	}
	if !errors.Is(err, &logdapi.SessionError{Code: logdapi.ErrCodeNotFound}) {
		t.Errorf("errors.Is(..., not_found) = false, want true (err: %v)", err)
	}
	// Wrapping must not cost the operator the context or the server's message.
	if msg := err.Error(); !strings.Contains(msg, "match error") || !strings.Contains(msg, "users/2") {
		t.Errorf("message lost context or detail: %q", msg)
	}
}

// TestPathErrorKindsCarryDistinctCodes guards the distinction the code makes and
// the message does not make reliably. logd's PathError has three kinds and they do
// not all mean the same thing to a caller deciding between "absent" and "broken":
//
//   - absent, and type-conflict, both mean nothing stands there — they resolve if
//     someone writes, so not_found is the honest answer and a retry is sensible;
//   - a bad segment is the CALLER's path being wrong. It can never resolve, so
//     answering not_found would promise "nothing there yet" about something no
//     write can fix.
func TestPathErrorKindsCarryDistinctCodes(t *testing.T) {
	srv := startLogd(t)
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "codes"})
	defer s.Close()
	ctx := context.Background()

	if _, err := s.Patch(ctx, "users/1", ir.FromMap(map[string]*ir.Node{
		"name": ir.FromString("test"),
	})); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		// A field that is simply not there.
		{"absent", "users/2", logdapi.ErrCodeNotFound},
		// A string sits where the path expects an object to descend into.
		{"type conflict", "users/1.name.x", logdapi.ErrCodePathConflict},
		// Extraction addresses object fields; an index is not one.
		// Something IS there, of a shape that cannot hold an index: neither "nothing
		// there" nor "not a well-formed question".
		{"shape conflict", "users/1[0]", logdapi.ErrCodePathConflict},
		// A wildcard names a set of values, and a read answers one.
		{"not a well-formed question", "users/1.*", logdapi.ErrCodeInvalidPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Match(ctx, tc.path)
			if err == nil {
				t.Fatalf("expected an error reading %q", tc.path)
			}
			if got := logdapi.ErrorCode(err); got != tc.want {
				t.Errorf("ErrorCode = %q, want %q (err: %v)", got, tc.want, err)
			}
		})
	}
}

// TestMatchFailedKeepsSentinelAndCode: doPatch's match_failed arm now wraps both the
// sentinel callers already use and the response it used to discard. Neither may
// regress — controller.go maps the sentinel back to a wire code.
func TestMatchFailedKeepsSentinelAndCode(t *testing.T) {
	srv := startLogd(t)
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "codes"})
	defer s.Close()
	ctx := context.Background()

	vNode := func(n int64) *ir.Node { return ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(n)}) }

	if _, err := s.Patch(ctx, "users/1", vNode(1)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Precondition v==2 against stored v==1.
	_, err := s.PatchIf(ctx, "users/1", vNode(3), &logdapi.PathData{Path: "users/1", Data: vNode(2)})
	if err == nil {
		t.Fatal("expected the precondition to fail")
	}
	if !errors.Is(err, ErrMatchFailed) {
		t.Errorf("errors.Is(..., ErrMatchFailed) = false, want true (err: %v)", err)
	}
	if got := logdapi.ErrorCode(err); got != logdapi.ErrCodeMatchFailed {
		t.Errorf("ErrorCode = %q, want %q (err: %v)", got, logdapi.ErrCodeMatchFailed, err)
	}
}

// TestErrorCodeIgnoresUnrelatedErrors: ErrorCode reports what a SERVER said. A
// local failure — a dial that never connected, a cancelled context — carries no
// code, and must not be reported as one lest a caller read "" as a code it knows.
func TestErrorCodeIgnoresUnrelatedErrors(t *testing.T) {
	if got := logdapi.ErrorCode(nil); got != "" {
		t.Errorf("ErrorCode(nil) = %q, want empty", got)
	}
	if got := logdapi.ErrorCode(errors.New("connection lost")); got != "" {
		t.Errorf("ErrorCode(plain) = %q, want empty", got)
	}
	if got := logdapi.ErrorCode(ErrMatchFailed); got != "" {
		t.Errorf("ErrorCode(bare sentinel) = %q, want empty", got)
	}
}
