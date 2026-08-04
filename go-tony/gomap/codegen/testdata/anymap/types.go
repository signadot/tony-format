package anymap

// map[string]any field — the shape from issue f69agjyeh12ks item 7.
//
//tony:schemagen=anymap-pattern,notag
type Pattern struct {
	Fields map[string]any `tony:"field=fields,omitzero"`
}
