package namedscalar

// Named scalar types — the shape from issue f69agjyeh12ks item 8.
type Verb string
type Count int

//tony:schemagen=namedscalar-gatematch,notag
type GateMatch struct {
	Verbs  []Verb   `tony:"field=verbs,omitzero"`
	Counts []Count  `tony:"field=counts,omitzero"`
	Names  []string `tony:"field=names,omitzero"`
}
