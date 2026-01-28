package mergeop

// PatchConfig holds user-facing behavioral options for patch operations.
type PatchConfig struct {
	// Comments controls whether comments are included in patch operations.
	Comments bool

	// RejectUnsafe rejects unsafe operations like !pipe that can execute
	// arbitrary system commands.
	RejectUnsafe bool
}

// PatchOpt is a functional option for configuring patch behavior.
type PatchOpt func(*PatchConfig)

// RejectUnsafe returns a PatchOpt that configures whether unsafe operations
// (like !pipe) should be rejected.
func RejectUnsafe(v bool) PatchOpt {
	return func(c *PatchConfig) { c.RejectUnsafe = v }
}

// Comments returns a PatchOpt that configures whether comments are included
// in patch operations.
func Comments(v bool) PatchOpt {
	return func(c *PatchConfig) { c.Comments = v }
}

// NewConfig creates a PatchConfig with the given options applied.
func NewConfig(opts ...PatchOpt) *PatchConfig {
	cfg := &PatchConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}
