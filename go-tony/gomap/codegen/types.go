package codegen

import (
	"go/ast"
	"reflect"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/schema"
)

// StructInfo holds parsed struct information from Go source
type StructInfo struct {
	// Name is the struct type name
	Name string

	// Package is the package name this struct belongs to
	Package string

	// FilePath is the path to the source file containing this struct
	FilePath string

	// Fields contains information about each struct field
	Fields []*FieldInfo

	// StructSchema holds schema tag information (schema= or schemadef=)
	StructSchema *gomap.StructSchema

	// Comments contains struct-level comments (above the type declaration)
	Comments []string

	// ASTNode is the original AST node for this struct (for reference)
	ASTNode *ast.StructType
}

// FieldInfo holds field information extracted from struct definition
type FieldInfo struct {
	// Name is the struct field name
	Name string

	// SchemaFieldName is the field name in the schema (may differ from struct field name)
	// Extracted from `tony:"field=name"` tag, or defaults to Name
	SchemaFieldName string

	// Type is the Go type of the field
	Type reflect.Type

	// ASTType is the AST representation of the field type (for codegen reference)
	ASTType ast.Expr

	// Optional indicates if the field is optional (nullable or can be empty)
	Optional bool

	// Required indicates if the field is required (overrides type-based inference)
	Required bool

	// Omit indicates if the field should be omitted from schema/code generation
	Omit bool

	// Comments contains field-level comments (above the field declaration)
	Comments []string

	// ASTField is the original AST field node (for reference)
	ASTField *ast.Field

	// IsEmbedded indicates if this is an embedded field
	IsEmbedded bool
}

// SchemaInfo holds schema metadata for code generation
type SchemaInfo struct {
	// Name is the schema name (from signature.name)
	Name string

	// Schema is the parsed schema object
	Schema *schema.Schema

	// FilePath is the path to the schema file (if loaded from filesystem)
	FilePath string
}

// PackageInfo holds information about a Go package
type PackageInfo struct {
	// Path is the package import path (e.g., "github.com/user/project/models")
	Path string

	// Dir is the directory containing the package
	Dir string

	// Name is the package name (e.g., "models")
	Name string

	// Files contains paths to all .go files in the package
	Files []string
}

// CodegenConfig holds configuration for code generation
type CodegenConfig struct {
	// OutputFile is the output file for generated Go code (default: <package>_gen.go)
	OutputFile string

	// SchemaDir is the directory for generated schema files (preserves package structure)
	SchemaDir string

	// SchemaDirFlat is the directory for generated schema files (flat structure)
	SchemaDirFlat string

	// Dir is the directory to scan for Go files (default: current directory)
	Dir string

	// Recursive indicates whether to scan subdirectories recursively
	Recursive bool

	// SchemaRegistry is the path to schema registry for cross-package references (optional)
	SchemaRegistry string

	// Package is the current package being processed
	Package *PackageInfo
}
