package storage

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A scope's !field onto a field it already holds is refused, not stored. It was stored as
// an object holding two fields of one name -- !insert.raw {b: 1, b: 2} in the log, a shape
// the IR cannot hold -- and at baseline the renamed value went missing (e5wt4fhxh12ksz5xmdn0).
func TestFieldOntoAHeldFieldIsRefused(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{a: 1, b: 2}`)
	s1 := "s1"
	for _, scope := range []*string{nil, &s1} {
		patch, err := parse.Parse([]byte(`!field(a,b) null`))
		if err != nil {
			t.Fatal(err)
		}
		txn, err := s.NewTx(1, scope)
		if err != nil {
			t.Fatal(err)
		}
		p, err := txn.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch}})
		if err != nil {
			continue // refused at the door is refused
		}
		if res := p.Commit(); res.Committed {
			t.Errorf("scope %v: the write committed at %d; want a refusal", scope, res.Commit)
		}
	}
	expectAt(t, s, nil, "", `{a: 1, b: 2}`)
}
