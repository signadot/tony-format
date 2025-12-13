package snap

import "github.com/signadot/tony-format/go-tony/ir/kpath"

type Path struct {
	kpath.KPath
}

func (p *Path) MarshalText() ([]byte, error) {
	return []byte(p.KPath.String()), nil
}

func (p *Path) UnmarshalText(d []byte) error {
	kp, err := kpath.Parse(string(d))
	if err != nil {
		return err
	}
	p.KPath = *kp
	return nil
}
