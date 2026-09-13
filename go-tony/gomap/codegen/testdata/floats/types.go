package floats

//tony:schemagen=floats-host,notag
type Host struct {
	F  float64            `tony:"field=f"`
	Xs []float64          `tony:"field=xs,omitzero"`
	M  map[string]float64 `tony:"field=m,omitzero"`
}
