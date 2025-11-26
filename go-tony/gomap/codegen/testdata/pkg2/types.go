package pkg2

//tony:schemagen=meta
type Meta struct {
	A int `tony:"field=a"`
}

//tony:schemagen=body
type Body struct {
	B string `tony:"field=b"`
}

//tony:schemagen=compound
type Compound struct {
	Meta Meta `tony:"field=meta"`
	Body Body `tony:"field=body"`
}
