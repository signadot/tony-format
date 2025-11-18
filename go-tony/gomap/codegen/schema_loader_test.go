package codegen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
)

func TestParseSchemaName(t *testing.T) {
	tests := []struct {
		name         string
		schemaName   string
		wantPkg      string
		wantLocal    string
	}{
		{
			name:       "local schema",
			schemaName: "person",
			wantPkg:    "",
			wantLocal:  "person",
		},
		{
			name:       "cross-package schema",
			schemaName: "models.person",
			wantPkg:    "models",
			wantLocal:  "person",
		},
		{
			name:       "nested package path",
			schemaName: "api.v1.user",
			wantPkg:    "api.v1",
			wantLocal:  "user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, local := parseSchemaName(tt.schemaName)
			if pkg != tt.wantPkg {
				t.Errorf("parseSchemaName(%q) pkg = %q, want %q", tt.schemaName, pkg, tt.wantPkg)
			}
			if local != tt.wantLocal {
				t.Errorf("parseSchemaName(%q) local = %q, want %q", tt.schemaName, local, tt.wantLocal)
			}
		})
	}
}

func TestValidateSchema(t *testing.T) {
	tests := []struct {
		name        string
		schema      *schema.Schema
		schemaName  string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid schema",
			schema: &schema.Schema{
				Signature: &schema.Signature{Name: "person"},
				Define:    make(map[string]*ir.Node),
			},
			schemaName: "person",
			wantErr:    false,
		},
		{
			name: "missing signature",
			schema: &schema.Schema{
				Define: make(map[string]*ir.Node),
			},
			schemaName:  "person",
			wantErr:     true,
			errContains: "missing signature.name",
		},
		{
			name: "missing define",
			schema: &schema.Schema{
				Signature: &schema.Signature{Name: "person"},
			},
			schemaName:  "person",
			wantErr:     true,
			errContains: "missing define map",
		},
		{
			name: "name mismatch",
			schema: &schema.Schema{
				Signature: &schema.Signature{Name: "user"},
				Define:    make(map[string]*ir.Node),
			},
			schemaName:  "person",
			wantErr:     true,
			errContains: "does not match",
		},
		{
			name: "cross-package name match",
			schema: &schema.Schema{
				Signature: &schema.Signature{Name: "person"},
				Define:    make(map[string]*ir.Node),
			},
			schemaName: "models.person",
			wantErr:    false, // Should match local name "person"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSchema(tt.schema, tt.schemaName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSchema() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateSchema() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestLoadSchema_FromGenerated(t *testing.T) {
	// Create a test schema IR node
	schemaIR := ir.FromMap(map[string]*ir.Node{
		"signature": ir.FromMap(map[string]*ir.Node{
			"name": ir.FromString("person"),
		}),
		"define": ir.FromMap(map[string]*ir.Node{
			"person": ir.FromMap(map[string]*ir.Node{
				"name": ir.FromString("!irtype string"),
				"age":  ir.FromString("!irtype number"),
			}),
		}),
	})

	// Create generated schemas map
	generatedSchemas := map[string]*GeneratedSchema{
		"person": {
			Name:    "person",
			IRNode:  schemaIR,
			FilePath: "person.tony",
		},
	}

	// Create config
	config := &CodegenConfig{
		Package: &PackageInfo{
			Name: "test",
			Dir:  "/tmp/test",
		},
		Dir: "/tmp/test",
	}

	cache := NewSchemaCache()

	// Load schema from generated schemas
	s, err := LoadSchema("person", "/tmp/test", config, cache, generatedSchemas)
	if err != nil {
		t.Fatalf("LoadSchema() error = %v", err)
	}

	if s == nil {
		t.Fatal("LoadSchema() returned nil schema")
	}

	if s.Signature == nil || s.Signature.Name != "person" {
		t.Errorf("LoadSchema() signature.name = %v, want 'person'", s.Signature)
	}

	// Verify it's cached
	if cached, ok := cache.schemas["person"]; !ok || cached != s {
		t.Error("LoadSchema() did not cache the schema")
	}
}

func TestResolveSchemaPath_Local(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	pkgDir := filepath.Join(tmpDir, "testpkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	// Create a test schema file
	schemaFile := filepath.Join(pkgDir, "person.tony")
	if err := os.WriteFile(schemaFile, []byte(`signature:
  name: person
define:
  person:
    name: !irtype string
`), 0644); err != nil {
		t.Fatalf("failed to create test schema file: %v", err)
	}

	config := &CodegenConfig{
		Package: &PackageInfo{
			Name: "testpkg",
			Dir:  pkgDir,
		},
		Dir: tmpDir,
	}

	// Test resolving local schema
	path, err := ResolveSchemaPath("person", pkgDir, config)
	if err != nil {
		t.Fatalf("ResolveSchemaPath() error = %v", err)
	}

	if path != schemaFile {
		t.Errorf("ResolveSchemaPath() = %q, want %q", path, schemaFile)
	}
}

func TestResolveSchemaPath_SchemaDir(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	schemaDir := filepath.Join(tmpDir, "schemas")
	pkgDir := filepath.Join(tmpDir, "testpkg")
	if err := os.MkdirAll(schemaDir, 0755); err != nil {
		t.Fatalf("failed to create schema directory: %v", err)
	}
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package directory: %v", err)
	}

	// Create a test schema file in schema directory
	schemaFile := filepath.Join(schemaDir, "testpkg", "person.tony")
	if err := os.MkdirAll(filepath.Dir(schemaFile), 0755); err != nil {
		t.Fatalf("failed to create schema subdirectory: %v", err)
	}
	if err := os.WriteFile(schemaFile, []byte(`signature:
  name: person
define:
  person:
    name: !irtype string
`), 0644); err != nil {
		t.Fatalf("failed to create test schema file: %v", err)
	}

	config := &CodegenConfig{
		SchemaDir: schemaDir,
		Package: &PackageInfo{
			Name: "testpkg",
			Dir:  pkgDir,
		},
		Dir: tmpDir,
	}

	// Test resolving schema from schema directory
	path, err := ResolveSchemaPath("person", pkgDir, config)
	if err != nil {
		t.Fatalf("ResolveSchemaPath() error = %v", err)
	}

	if path != schemaFile {
		t.Errorf("ResolveSchemaPath() = %q, want %q", path, schemaFile)
	}
}

func TestResolveSchemaPath_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	pkgDir := filepath.Join(tmpDir, "testpkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	config := &CodegenConfig{
		Package: &PackageInfo{
			Name: "testpkg",
			Dir:  pkgDir,
		},
		Dir: tmpDir,
	}

	// Test resolving non-existent schema
	_, err := ResolveSchemaPath("nonexistent", pkgDir, config)
	if err == nil {
		t.Error("ResolveSchemaPath() expected error for non-existent schema")
	}
}

func TestLoadSchema_FromFile(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	pkgDir := filepath.Join(tmpDir, "testpkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	// Create a test schema file
	schemaFile := filepath.Join(pkgDir, "person.tony")
	schemaContent := `signature:
  name: person
define:
  person:
    name: !irtype string
    age: !irtype number
`
	if err := os.WriteFile(schemaFile, []byte(schemaContent), 0644); err != nil {
		t.Fatalf("failed to create test schema file: %v", err)
	}

	config := &CodegenConfig{
		Package: &PackageInfo{
			Name: "testpkg",
			Dir:  pkgDir,
		},
		Dir: tmpDir,
	}

	cache := NewSchemaCache()

	// Load schema from file
	s, err := LoadSchema("person", pkgDir, config, cache, nil)
	if err != nil {
		t.Fatalf("LoadSchema() error = %v", err)
	}

	if s == nil {
		t.Fatal("LoadSchema() returned nil schema")
	}

	if s.Signature == nil || s.Signature.Name != "person" {
		t.Errorf("LoadSchema() signature.name = %v, want 'person'", s.Signature)
	}

	if s.Define == nil {
		t.Error("LoadSchema() returned schema without define map")
	}

	personDef, ok := s.Define["person"]
	if !ok {
		t.Error("LoadSchema() returned schema without 'person' definition")
	}

	if personDef == nil {
		t.Error("LoadSchema() returned schema with nil 'person' definition")
	}

	// Verify it's cached
	if cached, ok := cache.schemas["person"]; !ok || cached != s {
		t.Error("LoadSchema() did not cache the schema")
	}

	// Verify cache path
	if path, ok := cache.paths["person"]; !ok || path != schemaFile {
		t.Errorf("LoadSchema() cache path = %q, want %q", path, schemaFile)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || 
		(len(s) > len(substr) && (s[:len(substr)] == substr || 
		s[len(s)-len(substr):] == substr || 
		containsMiddle(s, substr))))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
