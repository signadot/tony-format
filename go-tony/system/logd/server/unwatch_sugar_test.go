package server

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A watch opened with element sugar is closed by the same spelling: the watch is held
// under the element's name, and an unwatch naming the sugar answered not_watching and
// left it open for the session's life (p478tacqh12krg32msn0 item 10).
func TestUnwatchBySugarClosesTheWatch(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	schema, err := parse.Parse([]byte(`{define: {items: {sku: !logd-key null}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetSchema(schema, false); err != nil {
		t.Fatalf("SetSchema: %s", err)
	}
	narrowWrite(t, store, "", `{items: [{sku: A, q: 1}]}`)

	for _, tc := range []struct{ name, watch, unwatch string }{
		{"by path", `{id: "w", watch: {path: "items(A)"}}`, `{id: "u", unwatch: {path: "items(A)"}}`},
		{"by id", `{id: "w", watch: {path: "items(A)"}}`, `{id: "u", unwatch: {path: "items(sku=A)", watchId: "w"}}`},
		{"id-less", `{watch: {path: "items(sku=A)"}}`, `{id: "u", unwatch: {path: "items(A)"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hub := NewWatchHub()
			byID := runRequests(t, store, hub, tc.watch, tc.unwatch)
			if r := byID["u"]; r == nil || r.Error != nil {
				t.Fatalf("unwatch answered %v, want the watch closed", r)
			}
			if n := hub.WatcherCount(); n != 0 {
				t.Errorf("%d watcher(s) still in the hub", n)
			}
		})
	}
}
