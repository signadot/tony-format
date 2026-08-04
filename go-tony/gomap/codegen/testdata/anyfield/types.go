package anyfield

//tony:schemagen=anyfield-box,notag
type Box struct {
	Any any `tony:"field=any,omitzero"`
}
