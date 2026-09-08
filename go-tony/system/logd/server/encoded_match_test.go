package server

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A match with no pattern is encoded from the store's event stream into the frame that
// goes out, and no node of it is built: a document whose events sum past the session's
// budget -- which is what Collect charges -- reads whole when its frame is under it,
// while the same read with a pattern, which needs the node, is refused for its budget.
// What the frame decodes to is what an unbudgeted Collect of the same read gives,
// comments included (rebuild_plan.md decision 3, phase 5).
func TestAMatchIsEncodedFromTheEventStream(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	// 400 entities: as events, well over a 20 KiB budget; on the wire, well under it.
	var sb strings.Builder
	sb.WriteString("{entities: {")
	for i := 0; i < 400; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("e" + strconv.Itoa(i) + ": {id: " + strconv.Itoa(i) + ", ok: true}")
	}
	sb.WriteString("}, note: 1 # a comment the store keeps\n}")
	narrowWrite(t, store, "verse", sb.String())
	commit, _ := store.GetCurrentCommit()

	const budget = 20 << 10
	session := NewSession("srv", newMockConn(), &SessionConfig{Storage: store, Hub: NewWatchHub(), ReadBudget: budget})
	id := "m"

	// The plain match: a frame, under the budget, decoding to the document.
	session.handleMatch(&id, &api.MatchRequest{PathData: api.PathData{Path: "verse"}})
	out := <-session.outgoing
	if out.frame == nil {
		t.Fatalf("a plain match was not encoded from the stream: %+v", out.resp)
	}
	if int64(len(out.frame)) > budget {
		t.Fatalf("the frame is %d bytes, over the budget of %d", len(out.frame), budget)
	}
	dec, err := stream.NewDecoder(bytes.NewReader(out.frame), stream.WithBrackets())
	if err != nil {
		t.Fatal(err)
	}
	node, err := stream.ReadDocument(dec)
	if err != nil {
		t.Fatalf("the frame does not decode: %v", err)
	}
	var resp api.SessionResponse
	if err := resp.FromTonyIR(node); err != nil {
		t.Fatalf("the frame is not a response: %v", err)
	}
	if resp.ID == nil || *resp.ID != id || resp.Result == nil || resp.Result.Match == nil || resp.Result.Match.Commit != commit {
		t.Fatalf("the frame decodes to %+v, want match %q at commit %d", resp, id, commit)
	}
	c, err := store.Read(commit, nil, "verse")
	if err != nil {
		t.Fatal(err)
	}
	want, err := storage.Collect(c, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if !api.SameState(resp.Result.Match.Body, want) {
		t.Errorf("the encoded body differs from the collected one\n got  %s\n want %s", render(resp.Result.Match.Body), render(want))
	}

	// With a pattern, the node is needed, and it is past the budget.
	session.handleMatch(&id, &api.MatchRequest{PathData: api.PathData{Path: "verse", Data: ir.FromMap(map[string]*ir.Node{"note": ir.Null()})}})
	out = <-session.outgoing
	if out.resp == nil || out.resp.Error == nil || !strings.Contains(out.resp.Error.Message, "budget") {
		t.Fatalf("a patterned read over the budget was not refused for it: frame %d bytes, resp %+v", len(out.frame), out.resp)
	}
}

func render(n *ir.Node) string {
	if n == nil {
		return "<nil>"
	}
	var b strings.Builder
	if err := encode.Encode(n, &b, encode.EncodeComments(true)); err != nil {
		return err.Error()
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}
