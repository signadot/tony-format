package namedstr

// Op is a named string with no MarshalText.
type Op string

//tony:schemagen=namedstr-cmd,notag
type Cmd struct {
	Op   Op     `tony:"field=op"`
	Ops  []Op   `tony:"field=ops,omitzero"`
	Name string `tony:"field=name,omitzero"`
}
