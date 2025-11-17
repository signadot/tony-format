package encode

import "github.com/signadot/tony-format/tony/format"

type EncodeOption func(*EncState)

func EncodeFormat(f format.Format) EncodeOption {
	return func(es *EncState) { es.format = f }
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
