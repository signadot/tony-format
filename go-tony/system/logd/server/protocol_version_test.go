package server

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// The protocol's safety rested on a deployment convention. A request field a server does
// not know is IGNORED, and an unread path is "" -- the whole document for a read, the ROOT
// for a write -- so a client one version ahead is not refused, it is answered wrongly, and
// the wrong answer looks like success. A mismatched pair was indistinguishable from a
// working one until something read the root (k0d4y1m6h12kr7cdgdn0).
func TestHandshakeRefusesAProtocolItDoesNotSpeak(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()

	t.Run("a version from the future is refused", func(t *testing.T) {
		resp := narrowRequest(t, store, `{id: "h", hello: {clientId: ahead, protocol: 99}}`)
		if resp.Error == nil {
			t.Fatalf("a client speaking protocol 99 was accepted: %+v", resp)
		}
		if resp.Error.Code != api.ErrCodeProtocolMismatch {
			t.Errorf("refused with %q, want %q", resp.Error.Code, api.ErrCodeProtocolMismatch)
		}
		if !strings.Contains(resp.Error.Message, "deploy them together") {
			t.Errorf("the refusal does not say what is wrong: %s", resp.Error.Message)
		}
	})

	t.Run("the current version is accepted, and answered with its own", func(t *testing.T) {
		resp := narrowRequest(t, store,
			`{id: "h", hello: {clientId: current, protocol: `+itoa(api.ProtocolVersion)+`}}`)
		if resp.Error != nil {
			t.Fatalf("the current protocol was refused: %s", resp.Error)
		}
		if resp.Result == nil || resp.Result.Hello == nil {
			t.Fatalf("no hello result: %+v", resp)
		}
		if got := resp.Result.Hello.Protocol; got != api.ProtocolVersion {
			t.Errorf("the server reports protocol %d, want %d", got, api.ProtocolVersion)
		}
	})

	t.Run("a client from before the check is accepted", func(t *testing.T) {
		resp := narrowRequest(t, store, `{id: "h", hello: {clientId: old}}`)
		if resp.Error != nil {
			t.Errorf("a client predating the version field was refused: %s", resp.Error)
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
