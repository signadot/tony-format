package libctl

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A retain through docd is routed rule by rule to the owner of each rule's container,
// and the result says who ran what: a base rule runs on logd, a rule under a mount is
// the controller's to run or refuse, and a base rule whose container has mounts beneath
// it says which it did not reach. Nothing is composed; the point is the report.
func TestRetainThroughDocdReportsWhoRanWhat(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "verse.ctl", newMemController())

	client := docdClient(t, docd, "client")
	ctx := context.Background()
	old := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	fresh := time.Now().Add(-time.Hour).Format(time.RFC3339)
	stamped := func(ts string) *ir.Node { return ir.FromMap(map[string]*ir.Node{"at": ir.FromString(ts)}) }
	for path, ts := range map[string]string{"jobs.a": old, "jobs.b": fresh, "verse.other": old} {
		if _, err := client.Patch(ctx, path, stamped(ts)); err != nil {
			t.Fatalf("patch %s: %s", path, err)
		}
	}

	res, err := client.Retain(ctx, &api.RetainRequest{What: []*api.RetainRule{
		{Path: "jobs.*", Age: ".at", After: "1d"},           // base: logd runs it
		{Path: "verse.ctl.runs.*", Age: ".at", After: "1d"}, // the controller's, which does not implement retain
		{Path: "verse.*", Age: ".at", After: "1d"},          // base, with a mount beneath it that the rule does not reach
	}})
	if err != nil {
		t.Fatalf("retain: %s", err)
	}
	if res.Now == "" {
		t.Error("the result names no now; docd fixes one for every owner")
	}
	if res.Deleted != 2 {
		t.Errorf("deleted %d, want 2 (jobs.a and verse.other)", res.Deleted)
	}
	if len(res.Rules) != 3 {
		t.Fatalf("rules = %+v, want 3 in the order sent", res.Rules)
	}
	if r := res.Rules[0]; r.Owner != "logd" || r.Deleted != 1 || r.Error != nil || len(r.Under) != 0 {
		t.Errorf("jobs.*: %+v, want run by logd, 1 deleted, nothing beneath", r)
	}
	if r := res.Rules[1]; r.Owner != "verse.ctl" || r.Error == nil || r.Error.Code != api.ErrCodeUnsupported || r.Deleted != 0 {
		t.Errorf("verse.ctl.runs.*: %+v, want refused by the controller at verse.ctl as unsupported", r)
	}
	if r := res.Rules[2]; r.Owner != "logd" || r.Deleted != 1 || len(r.Under) != 1 || r.Under[0] != "verse.ctl" {
		t.Errorf("verse.*: %+v, want run by logd, 1 deleted, verse.ctl beneath and unreached", r)
	}
	for path, want := range map[string]bool{"jobs.a": false, "jobs.b": true, "verse.other": false} {
		got, err := client.Match(ctx, path)
		present := err == nil && got != nil && got.Type != ir.NullType
		if present != want {
			t.Errorf("%s present = %v, want %v (err %v)", path, present, want, err)
		}
	}

	// Nothing ran anywhere: that is an error, with the owner's reason.
	_, err = client.Retain(ctx, &api.RetainRequest{What: []*api.RetainRule{{Path: "verse.ctl.runs.*", Age: ".at", After: "1d"}}})
	if err == nil {
		t.Fatal("a retain every owner refused answered a result, want an error")
	}
	if code := api.ErrorCode(err); code != api.ErrCodeUnsupported || !strings.Contains(err.Error(), "verse.ctl") {
		t.Errorf("refused with %q: %s; want %s naming verse.ctl", code, err, api.ErrCodeUnsupported)
	}

	// A malformed rule is refused by logd, and through docd that is the request's error
	// when it is the only rule.
	_, err = client.Retain(ctx, &api.RetainRequest{What: []*api.RetainRule{{Path: "jobs[*]", Age: ".at", After: "1d"}}})
	if code := api.ErrorCode(err); code != api.ErrCodeInvalidRetain {
		t.Errorf("jobs[*] through docd: %q: %v; want %s", code, err, api.ErrCodeInvalidRetain)
	}
}
