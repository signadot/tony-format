package index

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// The schema history: which schema was in force from which commit. It lives with the
// index because it is derived from the log the same way the index is -- Build notes every
// schema entry it walks -- and persisted with it, in the manifest, under the same
// MaxCommit: an entry the manifest covers is not walked again, so what it said about the
// schema has to be in the manifest too (090mbrhsh12ksfr8mhn0).

// SchemaAt is one schema and the commit it took effect at.
type SchemaAt struct {
	Commit int64
	Schema *ir.Node
}

// ManifestSchema is SchemaAt as the manifest holds it, the schema as tony text.
type ManifestSchema struct {
	Commit int64
	Text   string
}

// NoteSchema records that schema was in force from commit. Noting a commit already
// noted replaces what was noted for it: a root snapshot carries the schema commit's own
// commit and schema, so the two say the same thing. Called on the root.
func (i *Index) NoteSchema(commit int64, schema *ir.Node) {
	i.schemaMu.Lock()
	defer i.schemaMu.Unlock()
	at := sort.Search(len(i.schemas), func(k int) bool { return i.schemas[k].Commit >= commit })
	if at < len(i.schemas) && i.schemas[at].Commit == commit {
		i.schemas[at].Schema = schema
		return
	}
	i.schemas = append(i.schemas, SchemaAt{})
	copy(i.schemas[at+1:], i.schemas[at:])
	i.schemas[at] = SchemaAt{Commit: commit, Schema: schema}
}

// SchemaHistory answers every schema noted, in commit order.
func (i *Index) SchemaHistory() []SchemaAt {
	i.schemaMu.RLock()
	defer i.schemaMu.RUnlock()
	return append([]SchemaAt(nil), i.schemas...)
}

func (i *Index) manifestSchemas() ([]ManifestSchema, error) {
	i.schemaMu.RLock()
	defer i.schemaMu.RUnlock()
	out := make([]ManifestSchema, 0, len(i.schemas))
	for _, sa := range i.schemas {
		var text string
		if sa.Schema != nil {
			var buf bytes.Buffer
			if err := encode.Encode(sa.Schema, &buf); err != nil {
				return nil, fmt.Errorf("index: encoding the schema set at %d: %w", sa.Commit, err)
			}
			text = buf.String()
		}
		out = append(out, ManifestSchema{Commit: sa.Commit, Text: text})
	}
	return out, nil
}

func (i *Index) loadSchemas(ms []ManifestSchema) error {
	for _, m := range ms {
		var schema *ir.Node
		if m.Text != "" {
			node, err := parse.Parse([]byte(m.Text))
			if err != nil {
				return fmt.Errorf("index: the manifest's schema at %d does not parse: %w", m.Commit, err)
			}
			schema = node
		}
		i.NoteSchema(m.Commit, schema)
	}
	return nil
}
