package namedint

import "time"

// Level is a named integer scalar.
type Level int

//tony:schemagen=namedint-host,notag
type Host struct {
	L Level         `tony:"field=l"`
	P *Level        `tony:"field=p,omitzero"`
	D time.Duration `tony:"field=d,omitzero"`
}
