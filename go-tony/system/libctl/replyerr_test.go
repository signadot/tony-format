package libctl

import (
	"errors"
	"fmt"
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// replyErr answers with a code the client can act on, and the question it answers is what
// the failure was ABOUT.
//
// It used to default to invalid_message, which says the client's request was malformed.
// Every error it reports but one comes from a controller's handler failing, and the one
// that does not passes ErrUnsupported explicitly -- so the default blamed the request for
// the responder's failure, and a client acting on it would rewrite a request that was
// fine.
func TestReplyErrCode(t *testing.T) {
	sessErr := func(code string) error {
		return &api.SessionError{Code: code, Message: "downstream said so"}
	}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"a handler that failed and said nothing", errors.New("disk on fire"), api.ErrCodeStorage},
		{"a declined operation", fmt.Errorf("%w: watch", ErrUnsupported), api.ErrCodeUnsupported},
		{"a failed compare-and-swap", fmt.Errorf("cas: %w", ErrMatchFailed), api.ErrCodeMatchFailed},

		// Forwarded: these describe the document, and are as true for the client as
		// they were for the controller.
		{"nothing at the path", sessErr(api.ErrCodeNotFound), api.ErrCodeNotFound},
		{"the shape disagrees", sessErr(api.ErrCodePathConflict), api.ErrCodePathConflict},
		{"an unstorable patch", sessErr(api.ErrCodeInvalidDiff), api.ErrCodeInvalidDiff},
		{"a commit that is gone", sessErr(api.ErrCodeCommitNotFound), api.ErrCodeCommitNotFound},

		// Not forwarded: these describe a connection or a lifecycle the client is not
		// party to. Passing them on names a condition that has not happened.
		{"the controller's session closed", sessErr(api.ErrCodeSessionClosed), api.ErrCodeStorage},
		{"the controller sent a bad message", sessErr(api.ErrCodeInvalidMessage), api.ErrCodeStorage},
		{"a transaction the client has no part in", sessErr(api.ErrCodeTxNotFound), api.ErrCodeStorage},
		{"the controller's watch was dropped", sessErr(api.ErrCodeSlowConsumer), api.ErrCodeStorage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := replyErrCode(tc.err); got != tc.want {
				t.Errorf("code = %q, want %q", got, tc.want)
			}
		})
	}
}
