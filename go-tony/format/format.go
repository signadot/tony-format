package format

import (
	"errors"
	"fmt"
)

type Format int

const (
	TonyFormat Format = iota
	YAMLFormat
	JSONFormat
)

var ErrBadFormat = errors.New("bad format")

func ParseFormat(v string) (Format, error) {
	f, ok := map[string]Format{
		"t":    TonyFormat,
		"tony": TonyFormat,
		"y":    YAMLFormat,
		"yaml": YAMLFormat,
		"j":    JSONFormat,
		"json": JSONFormat,
	}[v]
	if ok {
		return f, nil
	}
	return 0, fmt.Errorf("%w: %q", ErrBadFormat, v)
}

func (f Format) String() string {
	d, err := f.MarshalText()
	if err != nil {
		return err.Error()
	}
	return string(d)
}

func (f Format) MarshalText() ([]byte, error) {
	switch f {
	case TonyFormat:
		return []byte("tony"), nil
	case YAMLFormat:
		return []byte("yaml"), nil
	case JSONFormat:
		return []byte("json"), nil
	default:
		return nil, fmt.Errorf("<err: %d is not a format>", f)
	}
}

func (f *Format) UnmarshalText(d []byte) error {
	pf, err := ParseFormat(string(d))
	if err != nil {
		return err
	}
	*f = pf
	return nil
}

func (f Format) IsJSON() bool { return f == JSONFormat }
func (f Format) IsTony() bool { return f == TonyFormat }
func (f Format) IsYAML() bool { return f == YAMLFormat }

// Suffix returns the file extension for this format (including the dot).
func (f Format) Suffix() string {
	switch f {
	case TonyFormat:
		return ".tony"
	case YAMLFormat:
		return ".yaml"
	case JSONFormat:
		return ".json"
	default:
		return ""
	}
}

// AllFormats returns all supported formats in preference order.
func AllFormats() []Format {
	return []Format{TonyFormat, YAMLFormat, JSONFormat}
}
