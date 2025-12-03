package gomap

import (
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/token"
)

// MapOption is an option for controlling the mapping process from Go to Tony IR.
type MapOption func(*mapConfig)

func EncodeWire(v bool) MapOption {
	return func(c *mapConfig) {
		c.EncodeOptions = append(c.EncodeOptions, encode.EncodeWire(v))
	}
}

// EncodeFormat sets the output format (Tony, YAML, or JSON).
func EncodeFormat(f format.Format) MapOption {
	return func(c *mapConfig) {
		c.EncodeOptions = append(c.EncodeOptions, encode.EncodeFormat(f))
	}
}

// Depth sets the indentation depth for encoding.
func Depth(n int) MapOption {
	return func(c *mapConfig) {
		c.EncodeOptions = append(c.EncodeOptions, encode.Depth(n))
	}
}

// EncodeComments controls whether comments are included in the encoded output.
func EncodeComments(v bool) MapOption {
	return func(c *mapConfig) {
		c.EncodeOptions = append(c.EncodeOptions, encode.EncodeComments(v))
	}
}

// InjectRaw controls whether raw values are injected in the encoded output.
func InjectRaw(v bool) MapOption {
	return func(c *mapConfig) {
		c.EncodeOptions = append(c.EncodeOptions, encode.InjectRaw(v))
	}
}

// EncodeColors sets the color scheme for encoded output.
func EncodeColors(c *encode.Colors) MapOption {
	return func(cfg *mapConfig) {
		cfg.EncodeOptions = append(cfg.EncodeOptions, encode.EncodeColors(c))
	}
}

// EncodeBrackets controls whether brackets are used in the encoded output.
func EncodeBrackets(v bool) MapOption {
	return func(c *mapConfig) {
		c.EncodeOptions = append(c.EncodeOptions, encode.EncodeBrackets(v))
	}
}

// UnmapOption is an option for controlling the unmapping process from Tony IR to Go.
type UnmapOption func(*unmapConfig)

// mapConfig holds configuration for the mapping process.
type mapConfig struct {
	// EncodeOptions to pass through to encode.Encode
	EncodeOptions []encode.EncodeOption

	// Add mapping-specific options here in the future, e.g.:
	// SkipZeroValues bool
	// UseMarshalText bool
	// CustomHandlers map[reflect.Type]func(interface{}) (*ir.Node, error)
}

// unmapConfig holds configuration for the unmapping process.
type unmapConfig struct {
	// ParseOptions to pass through to parse.Parse
	ParseOptions []parse.ParseOption

	// Add unmapping-specific options here in the future, e.g.:
	// StrictMode bool
	// CustomHandlers map[reflect.Type]func(*ir.Node, interface{}) error
}

// newMapConfig creates a new mapConfig with default values.
func newMapConfig() *mapConfig {
	return &mapConfig{
		EncodeOptions: nil,
	}
}

// newUnmapConfig creates a new unmapConfig with default values.
func newUnmapConfig() *unmapConfig {
	return &unmapConfig{
		ParseOptions: nil,
	}
}

// ToEncodeOptions extracts EncodeOptions from a slice of MapOptions.
func ToEncodeOptions(opts ...MapOption) []encode.EncodeOption {
	cfg := newMapConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg.EncodeOptions
}

// ToParseOptions extracts ParseOptions from a slice of UnmapOptions.
func ToParseOptions(opts ...UnmapOption) []parse.ParseOption {
	cfg := newUnmapConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg.ParseOptions
}

// ParseFormat sets the input format for parsing (Tony, YAML, or JSON).
func ParseFormat(f format.Format) UnmapOption {
	return func(cfg *unmapConfig) {
		cfg.ParseOptions = append(cfg.ParseOptions, parse.ParseFormat(f))
	}
}

// ParseYAML sets the input format to YAML.
func ParseYAML() UnmapOption {
	return ParseFormat(format.YAMLFormat)
}

// ParseTony sets the input format to Tony.
func ParseTony() UnmapOption {
	return ParseFormat(format.TonyFormat)
}

// ParseJSON sets the input format to JSON.
func ParseJSON() UnmapOption {
	return ParseFormat(format.JSONFormat)
}

// ParseComments controls whether comments are parsed from the input.
func ParseComments(v bool) UnmapOption {
	return func(cfg *unmapConfig) {
		cfg.ParseOptions = append(cfg.ParseOptions, parse.ParseComments(v))
	}
}

// ParsePositions enables position tracking during parsing.
// The provided map will be populated with node-to-position mappings.
func ParsePositions(m map[*ir.Node]*token.Pos) UnmapOption {
	return func(cfg *unmapConfig) {
		cfg.ParseOptions = append(cfg.ParseOptions, parse.ParsePositions(m))
	}
}

// NoBrackets disables bracket parsing.
func NoBrackets() UnmapOption {
	return func(cfg *unmapConfig) {
		cfg.ParseOptions = append(cfg.ParseOptions, parse.NoBrackets())
	}
}
