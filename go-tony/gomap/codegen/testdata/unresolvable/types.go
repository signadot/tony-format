package unresolvable

// Leaf has no schema directive and no codec — a reference to it cannot be
// resolved by codegen (issue f69agjyeh12ks item 3).
type Leaf struct{ V string }

//tony:schemagen=unresolvable-host,notag
type Host struct {
	ValLeaf Leaf `tony:"field=valLeaf"`
}
