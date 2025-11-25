package pkg

//tony:schemagen=inner
type Inner struct {
	I int `tony:"field=i"`
}

//tony:schemagen=outer
type Outer struct {
	Inner
	F float64 `tony:"field=f"`
}
