package pkg3

//tony:schema=inner
type Inner struct {
	A int
}

//tony:schemagen=outer
type Outer struct {
	Inner Inner
	F     string `tony:"field=f"`
}
