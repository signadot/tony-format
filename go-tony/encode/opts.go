package encode

import "github.com/signadot/tony-format/go-tony/format"

type EncodeOption func(*EncState)

func EncodeFormat(f format.Format) EncodeOption {
	return func(es *EncState) { es.format = f }
}

// FormatFromOpts extracts the format from encode options.
func FormatFromOpts(opts ...EncodeOption) format.Format {
	es := &EncState{}
	for _, opt := range opts {
		opt(es)
	}
	return es.format
}

// FormatSuffix returns the file extension for the given format.
func FormatSuffix(f format.Format) string {
	switch f {
	case format.JSONFormat:
		return ".json"
	case format.TonyFormat:
		return ".tony"
	default:
		return ".yaml"
	}
}
func Depth(n int) EncodeOption {
	return func(es *EncState) { es.depth = n }
}
func EncodeComments(v bool) EncodeOption {
	return func(es *EncState) { es.comments = v }
}
func InjectRaw(v bool) EncodeOption {
	return func(es *EncState) { es.injectRaw = v }
}
func EncodeColors(c *Colors) EncodeOption {
	return func(es *EncState) { es.Color = c.Color }
}
func EncodeWire(v bool) EncodeOption {
	return func(es *EncState) { es.wire = v }
}
func EncodeBrackets(v bool) EncodeOption {
	return func(es *EncState) { es.brackets = v }
}
func EncodeLiteral(v bool) EncodeOption {
	return func(es *EncState) { es.literal = v }
}
