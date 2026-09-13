package embedptr

//tony:schemagen=embedptr-base,notag
type Base struct {
	ID   string `tony:"field=id"`
	Note string `tony:"field=note,omitzero"`
}

// S promotes Base's fields through a pointer, which may be nil.
//
//tony:schemagen=embedptr-s,notag
type S struct {
	*Base
	Name string `tony:"field=name"`
}

// T declares an ID of its own, which shadows Base's.
//
//tony:schemagen=embedptr-t,notag
type T struct {
	Base
	ID string `tony:"field=id"`
}
