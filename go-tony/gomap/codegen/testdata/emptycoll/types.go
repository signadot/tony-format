package emptycoll

// Coll exercises empty-slice emission (issue f69agjyeh12ks item 10B): an untagged
// empty slice is emitted as [], while an omitzero one is dropped.
//
//tony:schemagen=emptycoll-coll,notag
type Coll struct {
	Items []string `tony:"field=items"`
	OZ    []string `tony:"field=oz,omitzero"`
}
