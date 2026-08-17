package ir

import (
	"encoding/json"
	"strings"
	"testing"
)

// An object holds one value per field. Unmarshalling checked neither direction:
// more values than fields PANICKED, reading y.Fields[i] off the end -- `o load`
// on a malformed file died with a stack trace where it should have reported one
// -- and more fields than values was accepted, producing a node whose fields and
// values were misaligned for everything downstream to trip over.
func TestUnmarshalRefusesAMalformedObject(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "more values than fields",
			src:  `{"type":"Object","fields":[],"values":[{"type":"Null"}]}`,
			want: "0 fields and 1 values",
		},
		{
			name: "more fields than values",
			src:  `{"type":"Object","fields":[{"type":"String","string":"a"}],"values":[]}`,
			want: "1 fields and 0 values",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked instead of reporting: %v", r)
				}
			}()
			var n Node
			err := json.Unmarshal([]byte(tc.src), &n)
			if err == nil {
				t.Fatalf("accepted %s", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not say %q", err, tc.want)
			}
		})
	}
}

// A null field key is a string key. The branch saying so was undone by the line
// after it, so the one shape it exists to accept was rejected with the very error
// it exists to prevent.
func TestUnmarshalTakesANullFieldKeyAsAString(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		ok   bool
	}{
		{
			name: "a null key beside a string key",
			src:  `{"type":"Object","fields":[{"type":"Null"},{"type":"String","string":"b"}],"values":[{"type":"Null"},{"type":"Null"}]}`,
			ok:   true,
		},
		{
			name: "two null keys",
			src:  `{"type":"Object","fields":[{"type":"Null"},{"type":"Null"}],"values":[{"type":"Null"},{"type":"Null"}]}`,
			ok:   true,
		},
		{
			name: "a number key beside a string key is still mixed",
			src:  `{"type":"Object","fields":[{"type":"Number","int64":1},{"type":"String","string":"b"}],"values":[{"type":"Null"},{"type":"Null"}]}`,
			ok:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var n Node
			err := json.Unmarshal([]byte(tc.src), &n)
			if tc.ok && err != nil {
				t.Errorf("refused: %v", err)
			}
			if !tc.ok && err == nil {
				t.Error("accepted mixed key types")
			}
		})
	}
}
