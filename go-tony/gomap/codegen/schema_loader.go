package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/schema"
)

// SchemaCache holds loaded schemas to avoid reloading
type SchemaCache struct {
	schemas map[string]*schema.Schema
	paths   map[string]string // schema name -> file path
}

// NewSchemaCache creates a new schema cache
func NewSchemaCache() *SchemaCache {
	return &SchemaCache{
		schemas: make(map[string]*schema.Schema),
		paths:   make(map[string]string),
	}
}

// LoadSchema loads a schema from filesystem or cache.
// schemaName can be:
//   - "person" (local schema, same package)
//   - "models.person" (cross-package reference)
//
// It tries to load from:
//   1. Cache (if already loaded)
//   2. Newly generated schemas (from Phase 2, same package)
//   3. Filesystem (relative to package, then module-relative)
//   4. Schema registry (if config.SchemaRegistry is set)
func LoadSchema(schemaName string, pkgPath string, config *CodegenConfig, cache *SchemaCache, generatedSchemas map[string]*GeneratedSchema) (*schema.Schema, error) {
	if cache == nil {
		cache = NewSchemaCache()
	}

	// Check cache first
	if cached, ok := cache.schemas[schemaName]; ok {
		return cached, nil
	}

	// Parse schema name (might be cross-package like "models.person")
	pkgName, localSchemaName := parseSchemaName(schemaName)

	// If it's a local schema (same package), check newly generated schemas first
	if pkgName == "" || pkgName == config.Package.Name {
		if genSchema, ok := generatedSchemas[localSchemaName]; ok {
			parsed, err := schema.ParseSchema(genSchema.IRNode)
			if err != nil {
				return nil, fmt.Errorf("failed to parse generated schema %q: %w", schemaName, err)
			}
			cache.schemas[schemaName] = parsed
			return parsed, nil
		}
	}

	// Find schema file path
	schemaPath, err := ResolveSchemaPath(schemaName, pkgPath, config)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve schema path for %q: %w", schemaName, err)
	}

	// Load and parse schema file
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file %q: %w", schemaPath, err)
	}

	// Parse Tony file to IR node
	irNode, err := parse.Parse(data, parse.ParseComments(true))
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema file %q: %w", schemaPath, err)
	}

	// Parse IR node to Schema
	s, err := schema.ParseSchema(irNode)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema from %q: %w", schemaPath, err)
	}

	// Validate schema
	if err := ValidateSchema(s, schemaName); err != nil {
		return nil, fmt.Errorf("schema validation failed for %q: %w", schemaName, err)
	}

	// Cache the schema
	cache.schemas[schemaName] = s
	cache.paths[schemaName] = schemaPath

	return s, nil
}

// GeneratedSchema holds a schema that was just generated (from Phase 2)
type GeneratedSchema struct {
	Name    string
	IRNode  *ir.Node
	FilePath string
}

// ResolveSchemaPath finds the path to a schema file.
// Tries multiple locations:
//   1. Same directory as package (for local schemas)
//   2. Schema directory (if config.SchemaDir is set, preserves structure)
//   3. Flat schema directory (if config.SchemaDirFlat is set)
//   4. Module-relative paths
//   5. Schema registry (if config.SchemaRegistry is set)
func ResolveSchemaPath(schemaName string, pkgPath string, config *CodegenConfig) (string, error) {
	// Parse schema name (might be cross-package like "models.person")
	pkgName, localSchemaName := parseSchemaName(schemaName)

	// Build expected filename
	expectedFilename := localSchemaName + ".tony"

	// Try 1: Same directory as package (for local schemas)
	if pkgName == "" || pkgName == config.Package.Name {
		localPath := filepath.Join(pkgPath, expectedFilename)
		if _, err := os.Stat(localPath); err == nil {
			return localPath, nil
		}
	}

	// Try 2: Schema directory (preserves structure)
	if config.SchemaDir != "" {
		var schemaPath string
		if pkgName != "" && pkgName != config.Package.Name {
			// Cross-package: preserve package structure
			schemaPath = filepath.Join(config.SchemaDir, pkgName, expectedFilename)
		} else {
			// Same package: use package directory structure
			if config.Package != nil {
				relPath, err := filepath.Rel(config.Dir, pkgPath)
				if err == nil {
					schemaPath = filepath.Join(config.SchemaDir, relPath, expectedFilename)
				} else {
					schemaPath = filepath.Join(config.SchemaDir, config.Package.Name, expectedFilename)
				}
			} else {
				schemaPath = filepath.Join(config.SchemaDir, expectedFilename)
			}
		}
		if _, err := os.Stat(schemaPath); err == nil {
			return schemaPath, nil
		}
	}

	// Try 3: Flat schema directory
	if config.SchemaDirFlat != "" {
		flatPath := filepath.Join(config.SchemaDirFlat, expectedFilename)
		if _, err := os.Stat(flatPath); err == nil {
			return flatPath, nil
		}
	}

	// Try 4: Schema registry (if set)
	if config.SchemaRegistry != "" {
		var registryPath string
		if pkgName != "" {
			// Cross-package: preserve package structure in registry
			registryPath = filepath.Join(config.SchemaRegistry, pkgName, expectedFilename)
		} else {
			// Same package: use package name in registry
			if config.Package != nil {
				registryPath = filepath.Join(config.SchemaRegistry, config.Package.Name, expectedFilename)
			} else {
				registryPath = filepath.Join(config.SchemaRegistry, expectedFilename)
			}
		}
		if _, err := os.Stat(registryPath); err == nil {
			return registryPath, nil
		}
	}

	return "", fmt.Errorf("schema file not found for %q (searched in package dir, schema-dir, schema-dir-flat, and schema-registry)", schemaName)
}

// parseSchemaName parses a schema name that might be cross-package.
// Examples:
//   - "person" -> ("", "person")
//   - "models.person" -> ("models", "person")
func parseSchemaName(schemaName string) (pkgName string, localName string) {
	parts := strings.Split(schemaName, ".")
	if len(parts) == 1 {
		return "", parts[0]
	}
	// Last part is the schema name, everything before is package path
	return strings.Join(parts[:len(parts)-1], "."), parts[len(parts)-1]
}

// ValidateSchema validates that a schema has the required structure.
func ValidateSchema(s *schema.Schema, schemaName string) error {
	// Check that schema has signature.name
	if s.Signature == nil || s.Signature.Name == "" {
		return fmt.Errorf("schema missing signature.name")
	}

	// Check that signature.name matches expected schema name
	// For cross-package references (e.g., "models.person"), we only check the local name
	_, localName := parseSchemaName(schemaName)
	if s.Signature.Name != localName {
		return fmt.Errorf("schema signature.name (%q) does not match expected name (%q)", s.Signature.Name, localName)
	}

	// Check that schema has define map (even if empty)
	if s.Define == nil {
		return fmt.Errorf("schema missing define map")
	}

	return nil
}
