package stream

// StreamOption configures Encoder/Decoder behavior.
type StreamOption func(*streamOpts)

type streamOpts struct {
	brackets bool // Force bracketed style
	wire     bool // Wire format (implies brackets)
}

// WithBrackets forces bracketed style encoding/decoding.
func WithBrackets() StreamOption {
	return func(opts *streamOpts) {
		opts.brackets = true
	}
}

// WithWire enables wire format (implies brackets).
func WithWire() StreamOption {
	return func(opts *streamOpts) {
		opts.wire = true
		opts.brackets = true // Wire format implies brackets
	}
}
