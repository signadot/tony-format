package server

import (
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A retain request is a writer: it reads a rule's container and commits an ordinary
// delete for the items whose own timestamp is older than the rule allows and whose
// match holds. What it does not delete is as much the contract as what it does, and
// logd holds no policy and no clock for it: the request carries the rules and the time.

func openStore(t *testing.T) *storage.Storage {
	t.Helper()
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open storage: %s", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// readWire answers the state at path, through a session, as a client sees it.
func readWire(t *testing.T, store *storage.Storage, path string) string {
	t.Helper()
	resp := narrowRequest(t, store, `{id: "r", match: {path: "`+path+`"}}`)
	if resp.Error != nil {
		return "error: " + resp.Error.Message
	}
	if resp.Result == nil || resp.Result.Match == nil {
		t.Fatalf("no match result: %+v", resp)
	}
	return wireOf(t, resp.Result.Match.Body)
}

// retain sends one retain request and answers its result, or fails on an error.
func retain(t *testing.T, store *storage.Storage, body string) *api.RetainResult {
	t.Helper()
	resp := narrowRequest(t, store, `{id: "x", retain: `+body+`}`)
	if resp.Error != nil {
		t.Fatalf("retain %s: %s: %s", body, resp.Error.Code, resp.Error.Message)
	}
	if resp.Result == nil || resp.Result.Retain == nil {
		t.Fatalf("no retain result: %+v", resp)
	}
	return resp.Result.Retain
}

// retainError sends one retain request that must fail, and answers the error.
func retainError(t *testing.T, store *storage.Storage, body string) *api.SessionError {
	t.Helper()
	resp := narrowRequest(t, store, `{id: "x", retain: `+body+`}`)
	if resp.Error == nil {
		t.Fatalf("retain %s succeeded: %+v", body, resp.Result)
	}
	return resp.Error
}

func stamp(d time.Duration) string { return time.Now().Add(d).Format(time.RFC3339) }

func TestRetainDeletesExpiredObjectItems(t *testing.T) {
	store := openStore(t)
	old, fresh := stamp(-48*time.Hour), stamp(-time.Hour)
	narrowWrite(t, store, "jobs.a", `{status: done, updatedAt: "`+old+`"}`)
	narrowWrite(t, store, "jobs.b", `{status: running, updatedAt: "`+old+`"}`) // does not match
	narrowWrite(t, store, "jobs.c", `{status: done, updatedAt: "`+fresh+`"}`)  // too young
	narrowWrite(t, store, "jobs.d", `{status: done}`)                          // no timestamp: kept
	narrowWrite(t, store, "jobs.e", `{status: done, updatedAt: yesterday}`)    // unreadable: kept
	narrowWrite(t, store, "jobs.f", `{status: canceled, updatedAt: "`+old+`"}`)

	req := `{what: [{path: "jobs.*", match: {status: !or [done, canceled]}, age: ".updatedAt", after: 1d}]}`
	before, _ := store.GetCurrentCommit()
	res := retain(t, store, req)
	if res.Deleted != 2 {
		t.Errorf("deleted %d items, want 2 (a and f)", res.Deleted)
	}
	if len(res.Rules) != 1 || res.Rules[0].Deleted != 2 || res.Rules[0].Unreadable != 2 {
		t.Errorf("rules = %+v, want one with 2 deleted and 2 unreadable", res.Rules)
	}
	if res.Now == "" {
		t.Error("result names no now, want the server's clock")
	}
	after, _ := store.GetCurrentCommit()
	if after != before+1 || res.Commit != after {
		t.Errorf("commits went %d -> %d, result says %d; want one delete commit", before, after, res.Commit)
	}
	got := readWire(t, store, "jobs")
	for _, kept := range []string{"b:", "c:", "d:", "e:"} {
		if !strings.Contains(got, kept) {
			t.Errorf("jobs = %s, want %s kept", got, kept)
		}
	}
	for _, gone := range []string{"a:", "f:"} {
		if strings.Contains(got, gone) {
			t.Errorf("jobs = %s, want %s deleted", got, gone)
		}
	}

	// Asked again, nothing is left to do, and nothing is committed.
	res = retain(t, store, req)
	if res.Deleted != 0 {
		t.Errorf("second request deleted %d, want 0", res.Deleted)
	}
	if again, _ := store.GetCurrentCommit(); again != after {
		t.Errorf("second request committed: %d -> %d", after, again)
	}
}

// The time is the caller's when it says so: given a now, the pass is a function of the
// state and the request, whatever the server's clock says.
func TestRetainMeasuresAgainstTheCallersNow(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "jobs.a", `{at: "2026-01-01T00:00:00Z"}`)
	narrowWrite(t, store, "jobs.b", `{at: "2026-06-01T00:00:00Z"}`)

	// As of March, only a is a month old.
	res := retain(t, store, `{now: "2026-03-01T00:00:00Z", what: [{path: "jobs.*", age: ".at", after: 30d}]}`)
	if res.Deleted != 1 || res.Now != "2026-03-01T00:00:00Z" {
		t.Errorf("result = %+v, want 1 deleted as of 2026-03-01", res)
	}
	if got, want := readWire(t, store, "jobs"), `{b: {at: "2026-06-01T00:00:00Z"}}`; got != want {
		t.Errorf("jobs = %s, want %s", got, want)
	}
	// A now that is not a time is refused before anything runs.
	if e := retainError(t, store, `{now: "March", what: [{path: "jobs.*", age: ".at", after: 30d}]}`); e.Code != api.ErrCodeInvalidRetain {
		t.Errorf("bad now: %s, want %s", e.Code, api.ErrCodeInvalidRetain)
	}
}

func TestRetainDeletesKeyedElementsByIdentity(t *testing.T) {
	store := openStore(t)
	schema, err := parse.Parse([]byte(`{define: {runs: {id: !logd-key null}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetSchema(schema, false); err != nil {
		t.Fatalf("SetSchema: %s", err)
	}
	old, fresh := stamp(-2*time.Hour), stamp(-time.Minute)
	narrowWrite(t, store, "", `{runs: [{id: r1, at: "`+old+`"}, {id: r2, at: "`+fresh+`"}, {id: r3, at: "`+old+`"}]}`)

	res := retain(t, store, `{what: [{path: "runs(*)", age: ".at", after: 1h}]}`)
	if res.Deleted != 2 {
		t.Errorf("deleted %d, want 2", res.Deleted)
	}
	if got, want := readWire(t, store, "runs"), `[{at: "`+fresh+`" id: r2}]`; got != want {
		t.Errorf("runs = %s, want %s", got, want)
	}

	// The rule has to say what the schema says: a keyed array's items are elements.
	e := retainError(t, store, `{what: [{path: "runs.*", age: ".at", after: 1h}]}`)
	if e.Code != api.ErrCodeInvalidRetain || !strings.Contains(e.Message, "runs(*)") {
		t.Errorf("runs.* over a keyed array: %s: %s; want %s naming runs(*)", e.Code, e.Message, api.ErrCodeInvalidRetain)
	}
}

func TestRetainDeletesSparseEntries(t *testing.T) {
	store := openStore(t)
	old, fresh := stamp(-2*time.Hour), stamp(-time.Minute)
	narrowWrite(t, store, "", `{events: {7: {at: "`+old+`"}, 9: {at: "`+fresh+`"}}}`)
	if res := retain(t, store, `{what: [{path: "events{*}", age: ".at", after: 1h}]}`); res.Deleted != 1 {
		t.Fatalf("deleted %d, want 1", res.Deleted)
	}
	if got, want := readWire(t, store, "events"), `!sparsearray {9: {at: "`+fresh+`"}}`; got != want {
		t.Errorf("events = %s, want %s", got, want)
	}
}

func TestRetainRefusesADenseArray(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "", `{list: [{at: "`+stamp(-2*time.Hour)+`"}]}`)

	// In the request: [*] is not a rule.
	e := retainError(t, store, `{what: [{path: "list[*]", age: ".at", after: 1h}]}`)
	if e.Code != api.ErrCodeInvalidRetain || !strings.Contains(e.Message, "keyed") {
		t.Errorf("list[*]: %s: %s; want refused, naming keyed", e.Code, e.Message)
	}
	// Against the store: what is there is a dense array, whatever the rule called it.
	e = retainError(t, store, `{what: [{path: "list(*)", age: ".at", after: 1h}]}`)
	if e.Code != api.ErrCodeInvalidRetain || !strings.Contains(e.Message, "no identity") {
		t.Errorf("list(*) unkeyed: %s: %s; want refused for no identity", e.Code, e.Message)
	}
	if got := readWire(t, store, "list"); !strings.Contains(got, "at:") {
		t.Errorf("list = %s, want untouched", got)
	}
}

func TestRetainDeletesInBatches(t *testing.T) {
	store := openStore(t)
	old := stamp(-2 * time.Hour)
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		narrowWrite(t, store, "log."+k, `{at: "`+old+`"}`)
	}
	before, _ := store.GetCurrentCommit()
	if res := retain(t, store, `{batch: 2, what: [{path: "log.*", age: ".at", after: 1h}]}`); res.Deleted != 5 {
		t.Fatalf("deleted %d, want 5", res.Deleted)
	}
	if after, _ := store.GetCurrentCommit(); after != before+3 {
		t.Errorf("commits went %d -> %d, want three batches of at most 2", before, after)
	}
	if got := readWire(t, store, "log"); got != "{}" {
		t.Errorf("log = %s, want empty", got)
	}
}

func TestRetainNestedAgeAndAbsentContainer(t *testing.T) {
	store := openStore(t)
	req := `{what: [{path: "log.*", age: "meta.at", after: 1h}]}`
	// Nothing at the path yet: a request is a no-op, not an error.
	if res := retain(t, store, req); res.Deleted != 0 {
		t.Fatalf("empty store: deleted %d", res.Deleted)
	}
	narrowWrite(t, store, "log.x", `{meta: {at: "`+stamp(-2*time.Hour)+`"}, v: 1}`)
	narrowWrite(t, store, "log.y", `{meta: {}, v: 2}`)
	if res := retain(t, store, req); res.Deleted != 1 || res.Rules[0].Unreadable != 1 {
		t.Fatalf("result = %+v, want 1 deleted, 1 unreadable", res)
	}
	if got, want := readWire(t, store, "log"), `{y: {meta: {} v: 2}}`; got != want {
		t.Errorf("log = %s, want %s", got, want)
	}
}

// A request that cannot mean what it says is refused whole, before any rule runs.
func TestRetainRefusesWhatCannotBeARule(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "jobs.a", `{at: "`+stamp(-2*time.Hour)+`"}`)
	for _, tc := range []struct{ name, body, want string }{
		{"no rules", `{what: []}`, "at least one rule"},
		{"concrete path", `{what: [{path: jobs, age: ".at", after: 1h}]}`, "names one node"},
		{"descent", `{what: [{path: "jobs..", age: ".at", after: 1h}]}`, "any depth"},
		{"wild in the middle", `{what: [{path: "a.*.jobs.*", age: ".at", after: 1h}]}`, "last segment"},
		{"no age", `{what: [{path: "jobs.*", after: 1h}]}`, "age is empty"},
		{"age not a field path", `{what: [{path: "jobs.*", age: "at[0]", after: 1h}]}`, "field path"},
		{"no after", `{what: [{path: "jobs.*", age: ".at"}]}`, "after"},
		{"after not a duration", `{what: [{path: "jobs.*", age: ".at", after: soon}]}`, "after"},
		{"match not an object", `{what: [{path: "jobs.*", age: ".at", after: 1h, match: !or [1, 2]}]}`, "object pattern"},
		{"match names the age field", `{what: [{path: "jobs.*", age: ".at", after: 1h, match: {at: x}}]}`, "age field"},
		{"negative batch", `{batch: -1, what: [{path: "jobs.*", age: ".at", after: 1h}]}`, "negative"},
		// The bad rule is second, and the good one before it does not run.
		{"a later bad rule refuses the request", `{what: [{path: "jobs.*", age: ".at", after: 1h}, {path: "x[*]", age: ".at", after: 1h}]}`, "keyed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := retainError(t, store, tc.body)
			if e.Code != api.ErrCodeInvalidRetain || !strings.Contains(e.Message, tc.want) {
				t.Errorf("%s: %s; want %s containing %q", e.Code, e.Message, api.ErrCodeInvalidRetain, tc.want)
			}
		})
	}
	if got := readWire(t, store, "jobs"); !strings.Contains(got, "a:") {
		t.Errorf("jobs = %s, want untouched", got)
	}
}

// A retain request in a scoped session reads the scope's view and deletes in the scope:
// baseline keeps the record.
func TestRetainInAScopeDeletesInTheScope(t *testing.T) {
	store := openStore(t)
	old := stamp(-2 * time.Hour)
	narrowWrite(t, store, "jobs.a", `{at: "`+old+`"}`)
	conn := newMockConn()
	conn.WriteRequest(`{hello: {clientId: t, scope: s1}}`)
	conn.WriteRequest(`{id: "x", retain: {what: [{path: "jobs.*", age: ".at", after: 1h}]}}`)
	conn.WriteRequest(`{id: "r", match: {path: "jobs"}}`)
	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: NewWatchHub()})
	done := make(chan error)
	go func() { done <- session.Run() }()
	time.Sleep(100 * time.Millisecond)
	conn.Close()
	<-done
	out := string(conn.GetResponses())
	if !strings.Contains(out, "deleted: 1") {
		t.Errorf("scoped retain: %s, want 1 deleted", out)
	}
	if strings.Contains(out, "a:") {
		t.Errorf("scoped read after retain: %s, want a gone in the scope", out)
	}
	if got := readWire(t, store, "jobs"); !strings.Contains(got, "a:") {
		t.Errorf("baseline jobs = %s, want a kept", got)
	}
}

// A retention limit is asked for in days and years, which time.ParseDuration does not
// read; d, w and y are calendar-blind multiples of an hour, and mix with the rest.
func TestParseDurationReadsDaysWeeksAndYears(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"1d", 24 * time.Hour},
		{"1.5d", 36 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"1y", 365 * 24 * time.Hour},
		{"1y6w", (365 + 42) * 24 * time.Hour},
		{"1d12h30m", 36*time.Hour + 30*time.Minute},
		{"90m", 90 * time.Minute},
		{"500ms", 500 * time.Millisecond},
	} {
		got, err := ParseDuration(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"d", "1x", "", "1dd", "3600"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("ParseDuration(%q) parsed, want refused", bad)
		}
	}
}

// The compaction section is refused at load for a policy the store would refuse --
// which used to fail inside every compaction instead -- and reads the horizon.
func TestCompactionConfigIsValidatedAtLoad(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"multiplier", "compaction: {multiplier: 1}\n", "Multiplier"},
		{"horizon inside cutoff", "compaction: {cutoff: 2h, horizon: 1h}\n", "Horizon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(writeConfig(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}
	cfg, err := LoadConfig(writeConfig(t, "compaction: {horizon: 2y}\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Compaction.ToStorageConfig().Horizon; got != 2*365*24*time.Hour {
		t.Errorf("horizon = %v, want 2y", got)
	}
}
