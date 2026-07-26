package nestedopts

//tony:schemagen=nestedopts-child,notag
type Child struct {
	V string `tony:"field=v"`
}

//tony:schemagen=nestedopts-host,notag
type Host struct {
	Child Child  `tony:"field=child"`
	Ptr   *Child `tony:"field=ptr"`
}
