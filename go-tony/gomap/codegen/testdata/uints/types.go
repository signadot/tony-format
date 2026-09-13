package uints

//tony:schemagen=uints-host,notag
type Host struct {
	U  uint64            `tony:"field=u"`
	N  uint32            `tony:"field=n,omitzero"`
	Xs []uint64          `tony:"field=xs,omitzero"`
	M  map[string]uint64 `tony:"field=m,omitzero"`
}
