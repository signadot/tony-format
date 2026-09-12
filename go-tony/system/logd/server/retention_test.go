package server

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// Retention is a writer: it reads a rule's container, and commits an ordinary delete
// for the items whose own timestamp is older than the rule allows and whose match
// holds. What it does not delete is as much the contract as what it does.

func retentionServer(t *testing.T, store *storage.Storage, cfg *RetentionConfig) *Server {
	t.Helper()
	return New(&Spec{
		Config:  &Config{Retention: cfg},
		Storage: store,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

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

func stamp(d time.Duration) string { return time.Now().Add(d).Format(time.RFC3339) }

func TestRetentionDeletesExpiredObjectItems(t *testing.T) {
	store := openStore(t)
	old, fresh := stamp(-48*time.Hour), stamp(-time.Hour)
	narrowWrite(t, store, "jobs.a", `{status: done, updatedAt: "`+old+`"}`)
	narrowWrite(t, store, "jobs.b", `{status: running, updatedAt: "`+old+`"}`) // does not match
	narrowWrite(t, store, "jobs.c", `{status: done, updatedAt: "`+fresh+`"}`)  // too young
	narrowWrite(t, store, "jobs.d", `{status: done}`)                          // no timestamp: kept
	narrowWrite(t, store, "jobs.e", `{status: done, updatedAt: yesterday}`)    // unreadable: kept
	narrowWrite(t, store, "jobs.f", `{status: canceled, updatedAt: "`+old+`"}`)

	srv := retentionServer(t, store, &RetentionConfig{Rules: []*RetentionRule{{
		Path:  "jobs.*",
		Match: mustParseNode(t, `{status: !or [done, canceled]}`),
		Age:   ".updatedAt",
		After: Duration(24 * time.Hour),
	}}})
	before, _ := store.GetCurrentCommit()
	deleted, err := srv.retentionPass(time.Now())
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted %d items, want 2 (a and f)", deleted)
	}
	after, _ := store.GetCurrentCommit()
	if after != before+1 {
		t.Errorf("commits went %d -> %d, want one delete commit", before, after)
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

	// A second pass finds nothing to do, and commits nothing.
	deleted, err = srv.retentionPass(time.Now())
	if err != nil || deleted != 0 {
		t.Errorf("second pass: deleted %d, err %v; want 0, nil", deleted, err)
	}
	if again, _ := store.GetCurrentCommit(); again != after {
		t.Errorf("second pass committed: %d -> %d", after, again)
	}
}

func TestRetentionDeletesKeyedElementsByIdentity(t *testing.T) {
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

	rule := &RetentionRule{Path: "runs(*)", Age: ".at", After: Duration(time.Hour)}
	srv := retentionServer(t, store, &RetentionConfig{Rules: []*RetentionRule{rule}})
	deleted, err := srv.retentionPass(time.Now())
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted %d, want 2", deleted)
	}
	if got, want := readWire(t, store, "runs"), `[{at: "`+fresh+`" id: r2}]`; got != want {
		t.Errorf("runs = %s, want %s", got, want)
	}

	// The rule has to say what the schema says: a keyed array's items are elements.
	srv = retentionServer(t, store, &RetentionConfig{Rules: []*RetentionRule{{Path: "runs.*", Age: ".at", After: Duration(time.Hour)}}})
	if _, err := srv.retentionPass(time.Now()); err == nil || !strings.Contains(err.Error(), "runs(*)") {
		t.Errorf("runs.* over a keyed array: err = %v, want one naming runs(*)", err)
	}
}

func TestRetentionDeletesSparseEntries(t *testing.T) {
	store := openStore(t)
	old, fresh := stamp(-2*time.Hour), stamp(-time.Minute)
	narrowWrite(t, store, "", `{events: {7: {at: "`+old+`"}, 9: {at: "`+fresh+`"}}}`)
	srv := retentionServer(t, store, &RetentionConfig{Rules: []*RetentionRule{{Path: "events{*}", Age: ".at", After: Duration(time.Hour)}}})
	if deleted, err := srv.retentionPass(time.Now()); err != nil || deleted != 1 {
		t.Fatalf("pass: deleted %d, err %v; want 1, nil", deleted, err)
	}
	if got, want := readWire(t, store, "events"), `!sparsearray {9: {at: "`+fresh+`"}}`; got != want {
		t.Errorf("events = %s, want %s", got, want)
	}
}

func TestRetentionRefusesADenseArray(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "", `{list: [{at: "`+stamp(-2*time.Hour)+`"}]}`)

	// At load: [*] is not a rule.
	r := &RetentionRule{Path: "list[*]", Age: ".at", After: Duration(time.Hour)}
	if _, err := r.target(); err == nil || !strings.Contains(err.Error(), "keyed") {
		t.Errorf("list[*]: err = %v, want refused, naming keyed", err)
	}
	// At run: what is there is a dense array, whatever the rule called it.
	srv := retentionServer(t, store, &RetentionConfig{Rules: []*RetentionRule{{Path: "list(*)", Age: ".at", After: Duration(time.Hour)}}})
	if _, err := srv.retentionPass(time.Now()); err == nil || !strings.Contains(err.Error(), "no identity") {
		t.Errorf("list(*) unkeyed: err = %v, want refused for no identity", err)
	}
	if got := readWire(t, store, "list"); !strings.Contains(got, "at:") {
		t.Errorf("list = %s, want untouched", got)
	}
}

func TestRetentionDeletesInBatches(t *testing.T) {
	store := openStore(t)
	old := stamp(-2 * time.Hour)
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		narrowWrite(t, store, "log."+k, `{at: "`+old+`"}`)
	}
	srv := retentionServer(t, store, &RetentionConfig{Batch: 2, Rules: []*RetentionRule{{Path: "log.*", Age: ".at", After: Duration(time.Hour)}}})
	before, _ := store.GetCurrentCommit()
	deleted, err := srv.retentionPass(time.Now())
	if err != nil || deleted != 5 {
		t.Fatalf("pass: deleted %d, err %v; want 5, nil", deleted, err)
	}
	if after, _ := store.GetCurrentCommit(); after != before+3 {
		t.Errorf("commits went %d -> %d, want three batches of at most 2", before, after)
	}
	if got := readWire(t, store, "log"); got != "{}" {
		t.Errorf("log = %s, want empty", got)
	}
}

func TestRetentionNestedAgeAndAbsentContainer(t *testing.T) {
	store := openStore(t)
	// Nothing at the path yet: a pass is a no-op, not an error.
	srv := retentionServer(t, store, &RetentionConfig{Rules: []*RetentionRule{{Path: "log.*", Age: "meta.at", After: Duration(time.Hour)}}})
	if deleted, err := srv.retentionPass(time.Now()); err != nil || deleted != 0 {
		t.Fatalf("empty store: deleted %d, err %v", deleted, err)
	}
	narrowWrite(t, store, "log.x", `{meta: {at: "`+stamp(-2*time.Hour)+`"}, v: 1}`)
	narrowWrite(t, store, "log.y", `{meta: {}, v: 2}`)
	if deleted, err := srv.retentionPass(time.Now()); err != nil || deleted != 1 {
		t.Fatalf("pass: deleted %d, err %v; want 1, nil", deleted, err)
	}
	if got, want := readWire(t, store, "log"), `{y: {meta: {} v: 2}}`; got != want {
		t.Errorf("log = %s, want %s", got, want)
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

// The config is refused at load for what cannot be a rule, and so is a compaction
// policy the store would refuse -- which used to fail inside every compaction instead.
func TestRetentionAndCompactionConfigAreValidatedAtLoad(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"no rules", "retention: {}\n", "no rules"},
		{"concrete path", "retention: {rules: [{path: jobs, age: .at, after: 1h}]}\n", "names one node"},
		{"dense array", "retention: {rules: [{path: 'jobs[*]', age: .at, after: 1h}]}\n", "keyed"},
		{"descent", "retention: {rules: [{path: 'jobs..', age: .at, after: 1h}]}\n", "any depth"},
		{"wild in the middle", "retention: {rules: [{path: 'a.*.jobs.*', age: .at, after: 1h}]}\n", "last segment"},
		{"no age", "retention: {rules: [{path: 'jobs.*', after: 1h}]}\n", "age is empty"},
		{"age not a field path", "retention: {rules: [{path: 'jobs.*', age: 'at[0]', after: 1h}]}\n", "field path"},
		{"no after", "retention: {rules: [{path: 'jobs.*', age: .at}]}\n", "positive duration"},
		{"match not an object", "retention: {rules: [{path: 'jobs.*', age: .at, after: 1h, match: !or [1, 2]}]}\n", "object pattern"},
		{"match names the age field", "retention: {rules: [{path: 'jobs.*', age: .at, after: 1h, match: {at: x}}]}\n", "age field"},
		{"compaction multiplier", "compaction: {multiplier: 1}\n", "Multiplier"},
		{"compaction horizon inside cutoff", "compaction: {cutoff: 2h, horizon: 1h}\n", "Horizon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(writeConfig(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}

	// And what is well formed loads, with the defaults filled in.
	cfg, err := LoadConfig(writeConfig(t, "retention:\n  rules:\n  - path: jobs.*\n    match: {status: !or [done, canceled]}\n    age: .updatedAt\n    after: 1d\n  - path: events(*)\n    age: .at\n    after: 1h\ncompaction: {horizon: 48h}\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	r := cfg.Retention
	if r == nil || len(r.Rules) != 2 || time.Duration(r.Every) != time.Hour || r.Batch != 256 || r.Author != "logd/retention" {
		t.Errorf("retention = %+v, want two rules and the defaults", r)
	}
	if time.Duration(r.Rules[0].After) != 24*time.Hour {
		t.Errorf("after = %v, want 24h", time.Duration(r.Rules[0].After))
	}
	if got := cfg.Compaction.ToStorageConfig().Horizon; got != 48*time.Hour {
		t.Errorf("horizon = %v, want 48h", got)
	}
}
