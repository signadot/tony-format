package gomap

import (
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// MapOption is an option for controlling the mapping process from Go to Tony IR.
type MapOption interface {
	applyMap(*mapConfig)
}

// UnmapOption is an option for controlling the unmapping process from Tony IR to Go.
type UnmapOption interface {
	applyUnmap(*unmapConfig)
}

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
		opt.applyMap(cfg)
	}
	return cfg.EncodeOptions
}

// ToParseOptions extracts ParseOptions from a slice of UnmapOptions.
func ToParseOptions(opts ...UnmapOption) []parse.ParseOption {
	cfg := newUnmapConfig()
	for _, opt := range opts {
		opt.applyUnmap(cfg)
	}
	return cfg.ParseOptions
}
