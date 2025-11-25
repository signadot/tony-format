package pkg3

import "errors"

//tony:schemagen=inner
type Inner struct {
	A A
}

type A int

func (a A) MarshalText() ([]byte, error) {
	return []byte("a"), nil
}

func (a *A) UnmarshalText(d []byte) error {
	if string(d) != "a" {
		return errors.New("not a")
	}
	var tmp A
	*a = tmp
	return nil
}
