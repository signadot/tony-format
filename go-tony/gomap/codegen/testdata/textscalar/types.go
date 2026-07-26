package textscalar

import "fmt"

// Ref is a named scalar whose wire form is text, not its underlying kind —
// the shape from issue f69agjyeh12ks that turned up alongside item 18.
type Ref uint64

func (r Ref) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("r%d", uint64(r))), nil
}

func (r *Ref) UnmarshalText(b []byte) error {
	var v uint64
	if _, err := fmt.Sscanf(string(b), "r%d", &v); err != nil {
		return err
	}
	*r = Ref(v)
	return nil
}

//tony:schemagen=textscalar-entity,notag
type Entity struct {
	Ref   Ref    `tony:"field=ref"`
	Refs  []Ref  `tony:"field=refs,omitzero"`
	Name  string `tony:"field=name,omitzero"`
	Count uint64 `tony:"field=count,omitzero"`
}
