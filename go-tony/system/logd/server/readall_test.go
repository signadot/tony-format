package server

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// readAll is the whole document at commit in the view scope names, as a server test reads
// it: the root cursor, collected under the default budget.
func readAll(t *testing.T, store *storage.Storage, commit int64, scope *string) *ir.Node {
	t.Helper()
	c, err := store.Read(commit, scope, "")
	if err != nil {
		t.Fatalf("read at %d: %v", commit, err)
	}
	doc, err := storage.Collect(c, DefaultReadBudget)
	if err != nil {
		t.Fatalf("collect at %d: %v", commit, err)
	}
	return doc
}
