package encode

import (
	"bytes"
	"strings"
	"testing"
)

// TestEncodeNilIsAnErrorNotACrash: a patch reports a deletion by returning a nil
// node, so a caller that writes a patch's result without checking arrives here
// with one. That used to dereference at the first field read -- a segfault that
// told the caller nothing about which of its documents had gone
// (issue a7bwkxwah12kr0n0fxn0).
func TestEncodeNilIsAnErrorNotACrash(t *testing.T) {
	var buf bytes.Buffer
	err := Encode(nil, &buf)
	if err == nil {
		t.Fatal("encoding a nil node succeeded; it is not a document")
	}
	if !strings.Contains(err.Error(), "nil node") {
		t.Errorf("error does not say what was wrong: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %q before failing", buf.String())
	}
}
