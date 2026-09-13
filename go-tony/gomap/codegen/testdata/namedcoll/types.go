package namedcoll

// Labels and Names are named collections with no directive and no codec of
// their own: a field of either is the collection, under its name.
type Labels map[string]string

type Names []string

//tony:schemagen=namedcoll-host,notag
type Host struct {
	Labels Labels `tony:"field=labels,omitzero"`
	Names  Names  `tony:"field=names,omitzero"`
}
