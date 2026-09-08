package storage

import (
	"bytes"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

func enc(t *testing.T, s string) string {
	t.Helper()
	n, err := parse.Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	var buf bytes.Buffer
	if err := encode.Encode(n, &buf, encode.EncodeWire(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.String()
}

// A keyed array cannot come to hold two elements with one name. The stored form makes it
// unrepresentable -- an object has one field per name -- and a write that would say
// otherwise is refused where it is written, whichever way it spells the array.
func TestKey_ReplaceWithDuplicateKeys(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {name: !logd-key null}}}`)
	mustCommit(t, s, nil, `{items: [{name: "a"}, {name: "b"}]}`)

	for _, write := range []string{
		`{items: [{name: "a"}, {name: "a"}]}`,
		`{items: !key(name) [{name: "a"}, {name: "a"}]}`,
		`{items: !replace {from: [{name: "a"}, {name: "b"}], to: [{name: "a"}, {name: "a"}]}}`,
	} {
		err := scopedCommit(t, s, nil, "", write)
		if err == nil || !strings.Contains(err.Error(), "names two elements") {
			t.Errorf("%s: want a refusal, got %v", enc(t, write), err)
		}
	}
	c, _ := s.GetCurrentCommit()
	if got := skus(mustReadScope(t, s, c, nil), "items"); len(got) != 2 {
		t.Errorf("after the refused writes the array holds %v", got)
	}
}
