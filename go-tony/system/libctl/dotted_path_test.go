package libctl

import (
	"context"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	docdserver "github.com/signadot/tony-format/go-tony/system/docd/server"
)

// docd's client face and logd have to agree about a path segment quoted because
// it contains a dot: `demo.probe."probe.dotted"` names one field, not two levels.
// r05ms7nch12ksxttgdn0 reports a verse store seeing them disagree -- a dotted id
// written nested, the read-back missing, a delete landing at the wrong address --
// and notes that verse's own suite cannot catch it, since it tests against a bare
// logd while everything a person runs goes through docd.  So the comparison lives
// here, against both faces, baseline and scoped.
func TestQuotedPathSegmentAgreesAcrossFaces(t *testing.T) {
	logdSrv := startLogd(t)

	docd := docdserver.New(&docdserver.Spec{LogdAddr: logdSrv.TCPAddr()})
	if err := docd.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("start docd: %s", err)
	}
	t.Cleanup(func() { docd.StopTCP() })
	if err := docd.StartClientTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("start docd client face: %s", err)
	}
	t.Cleanup(func() { docd.StopClientTCP() })

	// the three shapes the report distinguishes: a plain id, one with a dot (which
	// the client quotes), and one with a slash (which needs no quoting)
	ids := []string{"probe-plain", "probe.dotted", "probe/slashed"}

	for _, face := range []struct {
		name  string
		addr  string
		scope string
	}{
		{"logd", logdSrv.TCPAddr(), ""},
		{"docd", docd.ClientTCPAddr(), ""},
		{"logd scoped", logdSrv.TCPAddr(), "probe-scope"},
		{"docd scoped", docd.ClientTCPAddr(), "probe-scope"},
	} {
		t.Run(face.name, func(t *testing.T) {
			s := NewLogdSession(&LogdSessionConfig{
				Addr:     face.addr,
				ClientID: "probe",
				Scope:    face.scope,
			})
			defer s.Close()
			ctx := context.Background()
			if err := s.Connect(ctx); err != nil {
				t.Fatalf("connect: %s", err)
			}

			for _, id := range ids {
				path := "demo.probe." + quoteSegment(id)
				data := ir.FromMap(map[string]*ir.Node{"k": ir.FromString(id)})
				if _, err := s.Patch(ctx, path, data); err != nil {
					t.Fatalf("%s: patch: %s", path, err)
				}
				got, err := s.Match(ctx, path)
				if err != nil {
					t.Fatalf("%s: read back: %s", path, err)
				}
				if got == nil {
					t.Fatalf("%s: written and committed, and not there on the read back", path)
				}
			}

			// and the ids are the fields of one object, not a tree the dots dug
			doc, err := s.Match(ctx, "demo.probe")
			if err != nil {
				t.Fatalf("read demo.probe: %s", err)
			}
			if doc == nil || doc.Type != ir.ObjectType {
				t.Fatalf("demo.probe is %v", doc)
			}
			for _, id := range ids {
				if v := ir.Get(doc, id); v == nil {
					t.Errorf("demo.probe has no field %q; it holds %s", id, objectFields(doc))
				}
			}
		})
	}
}

// quoteSegment renders one path segment the way a client does: quoted when the
// name holds something a kpath would otherwise read as structure.
func quoteSegment(name string) string {
	return kpath.Field(name).String()
}

func objectFields(n *ir.Node) string {
	out := ""
	for i := range n.Fields {
		out += " " + n.Fields[i].String
	}
	return out
}
