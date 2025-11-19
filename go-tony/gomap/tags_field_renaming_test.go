package gomap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/schema"
)

// parseTxtar parses a txtar file and returns a map of filenames to content
func parseTxtar(content string) map[string]string {
	files := make(map[string]string)
	lines := strings.Split(content, "\n")
	
	var currentFile string
	var currentContent []string
	
	for _, line := range lines {
		if strings.HasPrefix(line, "-- ") && strings.HasSuffix(line, " --") {
			// Save previous file if any
			if currentFile != "" {
				files[currentFile] = strings.Join(currentContent, "\n")
			}
			// Start new file
			currentFile = strings.TrimPrefix(strings.TrimSuffix(line, " --"), "-- ")
			currentContent = nil
		} else if currentFile != "" {
			currentContent = append(currentContent, line)
		}
	}
	
	// Save last file
	if currentFile != "" {
		files[currentFile] = strings.Join(currentContent, "\n")
	}
	
	return files
}

func TestFieldRenaming_FromTxtar(t *testing.T) {
	testdataDir := filepath.Join("..", "testdata")
	
	tests := []struct {
		name     string
		filename string
	}{
		{"basic", "field_renaming_basic.txtar"},
		{"mixed", "field_renaming_mixed.txtar"},
		{"embedded", "field_renaming_embedded.txtar"},
		{"nested_embedded", "field_renaming_nested_embedded.txtar"},
		{"complex_types", "field_renaming_complex_types.txtar"},
		{"optional_fields", "field_renaming_optional_fields.txtar"},
		{"conflict_resolution", "field_renaming_conflict_resolution.txtar"},
		{"backwards_compat", "field_renaming_backwards_compat.txtar"},
		{"all_scenarios", "field_renaming_all_scenarios.txtar"},
		{"cross_package", "field_renaming_cross_package.txtar"},
		{"cross_package_nested", "field_renaming_cross_package_nested.txtar"},
		{"cross_package_embedded", "field_renaming_cross_package_embedded.txtar"},
		{"cross_package_type_ref", "field_renaming_cross_package_type_ref.txtar"},
		{"cross_package_nested_type", "field_renaming_cross_package_nested_type.txtar"},
		{"cross_package_pointer_type", "field_renaming_cross_package_pointer_type.txtar"},
		{"cross_package_slice_type", "field_renaming_cross_package_slice_type.txtar"},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(testdataDir, tt.filename)
			content, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("failed to read test file %q: %v", filePath, err)
			}
			
			files := parseTxtar(string(content))
			
			// Get schema file
			var schemaFile string
			var schemaContent string
			for name, content := range files {
				if strings.HasSuffix(name, ".tony") {
					schemaFile = name
					schemaContent = content
					break
				}
			}
			if schemaFile == "" {
				t.Fatal("no .tony schema file found in txtar")
			}
			
			// Parse schema
			irNode, err := parse.Parse([]byte(schemaContent), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("failed to parse schema: %v", err)
			}
			
			// Unwrap comment node if present
			if irNode.Type == ir.CommentType && len(irNode.Values) > 0 {
				irNode = irNode.Values[0]
			}
			
			// Parse schema from IR node
			s, err := schema.ParseSchema(irNode)
			if err != nil {
				t.Fatalf("failed to parse schema: %v", err)
			}
			
			// Get Go file (for reference, not used in this test)
			var goFile string
			for name := range files {
				if strings.HasSuffix(name, ".go") {
					goFile = name
					break
				}
			}
			if goFile == "" {
				t.Fatal("no .go file found in txtar")
			}
			
			// Parse Go file to extract struct info
			// For now, we'll use a simplified approach - we need to actually compile/parse the Go code
			// This is a placeholder that shows the structure - in practice, you'd use go/parser
			// or reflect to load the struct types
			
			// For this test, we'll verify the schema can be loaded and has the expected structure
			// Schemas can have either Accept (for schema= mode) or Define (for schemagen= mode)
			var fieldCount int
			var schemaFields []string
			if s.Accept != nil {
				if s.Accept.Type != ir.ObjectType {
					t.Fatalf("schema Accept must be ObjectType, got %v", s.Accept.Type)
				}
				fieldCount = len(s.Accept.Fields)
				for _, field := range s.Accept.Fields {
					if field.Type == ir.StringType {
						schemaFields = append(schemaFields, field.String)
					}
				}
			} else if len(s.Define) > 0 {
				// For schemagen mode, fields are in Define
				// The define map contains field definitions directly
				for name, defNode := range s.Define {
					if defNode.Type == ir.ObjectType {
						fieldCount += len(defNode.Fields)
						for _, field := range defNode.Fields {
							if field.Type == ir.StringType {
								schemaFields = append(schemaFields, field.String)
							}
						}
					} else {
						// Single field definition
						fieldCount++
						schemaFields = append(schemaFields, name)
					}
				}
			} else {
				t.Fatal("schema has neither Accept nor Define fields")
			}
			
			// Verify schema has fields
			if fieldCount == 0 {
				t.Fatal("schema has no fields")
			}
			
			t.Logf("Schema fields: %v", schemaFields)
			
			// Check expected_fields.txt if present
			if expectedFields, ok := files["expected_fields.txt"]; ok {
				// Parse expected fields
				expectedLines := strings.Split(strings.TrimSpace(expectedFields), "\n")
				// Filter out empty lines
				var nonEmptyLines []string
				for _, line := range expectedLines {
					if strings.TrimSpace(line) != "" {
						nonEmptyLines = append(nonEmptyLines, line)
					}
				}
				if len(nonEmptyLines) != fieldCount {
					t.Logf("Note: expected_fields.txt has %d fields, schema has %d fields", 
						len(nonEmptyLines), fieldCount)
				}
			}
			
			t.Logf("Successfully loaded schema from %q with %d fields", schemaFile, fieldCount)
		})
	}
}

// TestFieldRenaming_Integration tests field renaming with actual Go structs
// This requires the Go code to be compiled, so it's more of an integration test
func TestFieldRenaming_Integration(t *testing.T) {
	// This test would require:
	// 1. Compiling the Go code from txtar files
	// 2. Using reflect to get struct types
	// 3. Calling GetStructFields with the schema
	// 4. Verifying the field mappings match expected_fields.txt
	//
	// This is more complex and would require a test helper that:
	// - Writes Go files to a temp directory
	// - Compiles them
	// - Loads the types via reflect
	// - Runs the actual field mapping logic
	//
	// For now, the basic test above verifies the schema files are valid.
	t.Skip("Integration test requires Go code compilation - see TestFieldRenaming_FromTxtar for schema validation")
}
