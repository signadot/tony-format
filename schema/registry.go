package schema

import (
	"fmt"
	"sync"
)

var (
	mu       sync.RWMutex
	registry = make(map[string]*Schema)
)

// Register registers a schema in the global registry
func Register(s *Schema) error {
	if s == nil {
		return fmt.Errorf("cannot register nil schema")
	}
	if s.Signature == nil {
		return fmt.Errorf("schema must have a signature")
	}
	if s.Signature.Name == "" {
		return fmt.Errorf("schema signature must have a name")
	}

	mu.Lock()
	defer mu.Unlock()

	if _, exists := registry[s.Signature.Name]; exists {
		return fmt.Errorf("schema %q already registered", s.Signature.Name)
	}

	registry[s.Signature.Name] = s
	return nil
}

// Lookup looks up a schema by name
func Lookup(name string) *Schema {
	mu.RLock()
	defer mu.RUnlock()
	s := registry[name]
	return s
}

// All returns all registered schemas
func All() map[string]*Schema {
	mu.RLock()
	defer mu.RUnlock()

	result := make(map[string]*Schema, len(registry))
	for k, v := range registry {
		result[k] = v
	}
	return result
}
