package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/schema"
)

// SchemaCache caches loaded schemas to avoid redundant parsing
type SchemaCache struct {
	schemas map[string]*schema.Schema
}

// NewSchemaCache creates a new schema cache
func NewSchemaCache() *SchemaCache {
	return &SchemaCache{
		schemas: make(map[string]*schema.Schema),
	}
}

// GeneratedSchema holds information about a schema that was just generated in memory
type GeneratedSchema struct {
	Name     string
	FilePath string // Path where it will be/was written
	// We might add the *ir.Node here if we want to skip reloading from disk
	// But schema.Load parses from file, so we might need to write it first or convert IR to Schema
	// For now, let's assume we load from disk after writing, or we need a way to convert IR -> Schema
	// actually, schema.Schema is the parsed representation. ir.Node is the lower level.
	// Converting ir.Node to schema.Schema in memory is possible but might be complex.
	// Simpler to write to file then load, or just track the path.
	IRNode interface{} // Placeholder if we need it
}

// LoadSchema loads a schema by name.
// It checks:
// 1. The generatedSchemas map (for schemas just generated in this run)
// 2. The schemaCache (for already loaded schemas)
// 3. The filesystem (resolving the path)
func LoadSchema(schemaName string, pkgDir string, config *CodegenConfig, cache *SchemaCache, generatedSchemas map[string]*GeneratedSchema) (*schema.Schema, error) {
	// Check cache first
	if s, ok := cache.schemas[schemaName]; ok {
		return s, nil
	}

	// Check if it was just generated
	var schemaPath string
	if gen, ok := generatedSchemas[schemaName]; ok {
		schemaPath = gen.FilePath
	} else {
		// Resolve path from filesystem
		var err error
		schemaPath, err = ResolveSchemaPath(schemaName, pkgDir, config)
		if err != nil {
			return nil, err
		}
	}

	// Load from file
	// Read file contents
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file %s: %w", schemaPath, err)
	}

	// Parse Tony format to IR node(s)
	// The file might contain multiple schemas separated by ---
	// We need to split by --- and parse each document
	documents := splitDocuments(string(data))

	var s *schema.Schema
	for i, doc := range documents {
		// Skip empty documents
		if len(strings.TrimSpace(doc)) == 0 {
			continue
		}

		node, err := parse.Parse([]byte(doc))
		if err != nil {
			return nil, fmt.Errorf("failed to parse document %d in schema file %s: %w", i, schemaPath, err)
		}

		// Convert IR node to Schema
		parsedSchema, err := schema.ParseSchema(node)
		if err != nil {
			return nil, fmt.Errorf("failed to parse schema from document %d in %s: %w", i, schemaPath, err)
		}

		// Check if this is the schema we're looking for
		if parsedSchema.Signature != nil && parsedSchema.Signature.Name == schemaName {
			s = parsedSchema
			break
		}
	}

	if s == nil {
		return nil, fmt.Errorf("schema %q not found in file %s", schemaName, schemaPath)
	}

	// Add to cache
	cache.schemas[schemaName] = s
	return s, nil
}

// ResolveSchemaPath resolves the filesystem path for a schema name.
// schemaName can be:
// - "person" (local schema)
// - "models.person" (cross-package reference? handled by registry or convention)
// - "./schemas/person.tony" (explicit path? usually not in tag)
//
// Search order:
// 1. If config.SchemaDir is set, look there (preserving package structure?)
// 2. If config.SchemaDirFlat is set, look there
// 3. Look in current package directory (pkgDir)
// 4. Look in schema registry (if configured)
func ResolveSchemaPath(schemaName string, pkgDir string, config *CodegenConfig) (string, error) {
	// Handle simple case: local schema "person" -> "person.tony"
	fileName := schemaName + ".tony"

	// 1. Check SchemaDirFlat
	if config.SchemaDirFlat != "" {
		path := filepath.Join(config.SchemaDirFlat, fileName)
		if fileExists(path) {
			return path, nil
		}
	}

	// 2. Check SchemaDir
	// This is tricky because we don't know the relative path of the schema's package
	// unless schemaName implies it (e.g. "models.person").
	// If schemaName is just "person", we assume it's in the current package or same dir.
	if config.SchemaDir != "" {
		// Try joining SchemaDir + pkgDir's relative path?
		// Or just check SchemaDir/fileName?
		// If we are generating into SchemaDir, we expect to find it there.
		// But if we are loading a schema from *another* package, we need to know that package's path.

		// For now, let's assume local schema first.
		// If we are in pkgDir, and we generated to SchemaDir/pkgRel/person.tony
		// We need to reconstruct that path.
		// But ResolveSchemaPath is generic.

		// Let's try simple check in SchemaDir (if flat-ish usage)
		path := filepath.Join(config.SchemaDir, fileName)
		if fileExists(path) {
			return path, nil
		}
	}

	// 3. Check current package directory (default)
	localPath := filepath.Join(pkgDir, fileName)
	if fileExists(localPath) {
		return localPath, nil
	}

	// 4. Check Schema Registry (if configured)
	if config.SchemaRegistry != "" {
		path := filepath.Join(config.SchemaRegistry, fileName)
		if fileExists(path) {
			return path, nil
		}
	}

	// 5. Cross-package references?
	// If schemaName has dots, e.g. "models.person"
	// We might expect "models/person.tony" relative to module root?
	// This requires knowing module root.
	// For now, fail if not found locally.

	return "", fmt.Errorf("schema %q not found in %s or configured directories", schemaName, pkgDir)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

// splitDocuments splits a multi-document Tony file by --- separators
func splitDocuments(content string) []string {
	// Split by lines starting with ---
	lines := strings.Split(content, "\n")
	var documents []string
	var currentDoc strings.Builder

	for _, line := range lines {
		// Check if line is a document separator
		if strings.TrimSpace(line) == "---" {
			// Save current document if not empty
			if currentDoc.Len() > 0 {
				documents = append(documents, currentDoc.String())
				currentDoc.Reset()
			}
			continue
		}
		currentDoc.WriteString(line)
		currentDoc.WriteString("\n")
	}

	// Add the last document
	if currentDoc.Len() > 0 {
		documents = append(documents, currentDoc.String())
	}

	return documents
}
