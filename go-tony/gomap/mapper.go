package gomap

import (
	"github.com/signadot/tony-format/go-tony/schema"
)

var (
	// defaultSchemaRegistry is the default schema registry used when nil is passed to NewMapper
	defaultSchemaRegistry *schema.SchemaRegistry

	// defaultContextRegistry is the default context registry used when nil is passed to NewMapper
	defaultContextRegistry *schema.ContextRegistry
)

// SetDefaultRegistries sets the default registries used when nil is passed to NewMapper.
// This allows global configuration of schema registries.
func SetDefaultRegistries(schemaReg *schema.SchemaRegistry, ctxReg *schema.ContextRegistry) {
	defaultSchemaRegistry = schemaReg
	defaultContextRegistry = ctxReg
}

// GetDefaultRegistries returns the current default registries.
func GetDefaultRegistries() (*schema.SchemaRegistry, *schema.ContextRegistry) {
	return defaultSchemaRegistry, defaultContextRegistry
}

// Mapper provides schema-aware marshaling and unmarshaling.
// It holds optional schema and context registries for resolving schema references.
// If registries are nil, the default registries are used (set via SetDefaultRegistries).
type Mapper struct {
	schemaRegistry *schema.SchemaRegistry
	contextRegistry *schema.ContextRegistry
}

// NewMapper creates a new Mapper with the given registries.
// If either registry is nil, the corresponding default registry is used.
// If no default registries are set, schema resolution will be disabled for that registry.
func NewMapper(schemaReg *schema.SchemaRegistry, ctxReg *schema.ContextRegistry) *Mapper {
	m := &Mapper{}

	// Use provided registries, or fall back to defaults
	if schemaReg != nil {
		m.schemaRegistry = schemaReg
	} else {
		m.schemaRegistry = defaultSchemaRegistry
	}

	if ctxReg != nil {
		m.contextRegistry = ctxReg
	} else {
		m.contextRegistry = defaultContextRegistry
	}

	return m
}

// DefaultMapper returns a Mapper using the default registries.
// This is a convenience function equivalent to NewMapper(nil, nil).
func DefaultMapper() *Mapper {
	return NewMapper(nil, nil)
}

// SchemaRegistry returns the schema registry being used by this mapper.
func (m *Mapper) SchemaRegistry() *schema.SchemaRegistry {
	return m.schemaRegistry
}

// ContextRegistry returns the context registry being used by this mapper.
func (m *Mapper) ContextRegistry() *schema.ContextRegistry {
	return m.contextRegistry
}
