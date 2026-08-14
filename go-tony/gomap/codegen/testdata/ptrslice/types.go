package ptrslice

// PS exercises a pointer to a slice, which is how a field says three things
// rather than two: absent (no opinion), empty (an explicit none), and present.
// A plain slice collapses two of those into one whichever way the tag goes --
// with omitzero an empty slice is not emitted, without it nil and empty both
// emit [] -- so the pointer is the only shape that survives a round trip.
//
//tony:schemagen=ptrslice-ps,notag
type PS struct {
	Probe *[]string `tony:"field=probe,omitzero"`
	Ports *[]int    `tony:"field=ports,omitzero"`
	Steps *[]Step   `tony:"field=steps,omitzero"`
	Plain []string  `tony:"field=plain,omitzero"`
}

// Step is a struct element, so the slice decoder goes through FromTonyIR
// rather than the primitive path.
//
//tony:schemagen=ptrslice-step,notag
type Step struct {
	Name string `tony:"field=name"`
}
