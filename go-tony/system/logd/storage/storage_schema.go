package storage

import (
	"sync"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// schemaHistory is the store's schemas in commit order: which was in force from which
// commit. The last is the active one. It is loaded from the index's history at open
// (index.SchemaHistory) and appended to by SetSchema; the parsed forms are kept beside
// the documents, since key derivation consults one on every commit.
type schemaHistory struct {
	mu     sync.RWMutex
	at     []index.SchemaAt
	parsed []*api.Schema
}

func newSchemaHistory(history []index.SchemaAt) *schemaHistory {
	h := &schemaHistory{}
	for _, sa := range history {
		h.append(sa.Commit, sa.Schema)
	}
	return h
}

func (h *schemaHistory) append(commit int64, schema *ir.Node) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.at = append(h.at, index.SchemaAt{Commit: commit, Schema: schema})
	h.parsed = append(h.parsed, api.ParseSchemaFromNode(schema))
}

// Active answers the schema in force now and the commit that set it: nil and 0 for a
// store that has never been given one.
func (h *schemaHistory) Active() (*ir.Node, int64) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.at) == 0 {
		return nil, 0
	}
	last := h.at[len(h.at)-1]
	return last.Schema, last.Commit
}

// ActiveParsed is Active in the form key derivation needs, nil for a schemaless store.
func (h *schemaHistory) ActiveParsed() *api.Schema {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.parsed) == 0 {
		return nil
	}
	return h.parsed[len(h.parsed)-1]
}

// At answers the schema in force at commit and the commit that set it: the last one set
// at or before it, or nil and 0 when none was.
func (h *schemaHistory) At(commit int64) (*ir.Node, int64) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if i := h.indexAt(commit); i >= 0 {
		return h.at[i].Schema, h.at[i].Commit
	}
	return nil, 0
}

// ParsedAt is At in the form key derivation needs: the schema a commit's data was
// lowered under, which is the one that raises it (raise.go).
func (h *schemaHistory) ParsedAt(commit int64) *api.Schema {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if i := h.indexAt(commit); i >= 0 {
		return h.parsed[i]
	}
	return nil
}

// indexAt is the position of the schema in force at commit, or -1. Caller holds mu.
func (h *schemaHistory) indexAt(commit int64) int {
	for i := len(h.at) - 1; i >= 0; i-- {
		if h.at[i].Commit <= commit {
			return i
		}
	}
	return -1
}
