package gomap

import (
	"reflect"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
)

// schemaTag is a helper type for anonymous fields in test structs
type schemaTag struct{}

// schemaTag2 is another helper type for multiple schema tag tests
type schemaTag2 struct{}

func TestParseStructTag(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "empty tag",
			tag:  "",
			want: map[string]string{},
		},
		{
			name: "single key-value",
			tag:  "schema=person",
			want: map[string]string{"schema": "person"},
		},
		{
			name: "multiple key-values",
			tag:  "schema=person,allowExtra",
			want: map[string]string{"schema": "person", "allowExtra": ""},
		},
		{
			name: "with spaces",
			tag:  "schema=person, allowExtra",
			want: map[string]string{"schema": "person", "allowExtra": ""},
		},
		{
			name: "field name override",
			tag:  "field=name,required",
			want: map[string]string{"field": "name", "required": ""},
		},
		{
			name: "omit flag",
			tag:  "omit",
			want: map[string]string{"omit": ""},
		},
		{
			name: "dash as omit",
			tag:  "-",
			want: map[string]string{"-": ""},
		},
		{
			name: "complex example",
			tag:  "field=email,optional",
			want: map[string]string{"field": "email", "optional": ""},
		},
		{
			name: "quoted value with spaces",
			tag:  "field='full name'",
			want: map[string]string{"field": "full name"},
		},
		{
			name: "quoted value with spaces and other keys",
			tag:  "field='full name',required",
			want: map[string]string{"field": "full name", "required": ""},
		},
		{
			name: "double quoted value",
			tag:  `field="full name"`,
			want: map[string]string{"field": "full name"},
		},
		{
			name: "multiple quoted values",
			tag:  `field='full name',help='This is a description'`,
			want: map[string]string{"field": "full name", "help": "This is a description"},
		},
		{
			name: "quoted value with comma inside",
			tag:  `field='name, with comma'`,
			want: map[string]string{"field": "name, with comma"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStructTag(tt.tag)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseStructTag() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("ParseStructTag() = %v, want %v", got, tt.want)
				return
			}

			for k, v := range tt.want {
				if gotVal, ok := got[k]; !ok || gotVal != v {
					t.Errorf("ParseStructTag()[%q] = %q, want %q", k, gotVal, v)
				}
			}
		})
	}
}

func TestGetStructSchema(t *testing.T) {
	type Person struct {
		schemaTag `tony:"schema=person"`
		Name      string
		Age       int
	}

	type PersonDef struct {
		schemaTag `tony:"schemagen=person"`
		Name      string
		Age       int
	}

	type PersonWithExtra struct {
		schemaTag `tony:"schema=person,allowExtra"`
		Name      string
		Age       int
	}

	type NoSchema struct {
		Name string
		Age  int
	}

	type MultipleSchemas struct {
		schemaTag  `tony:"schema=person"`
		schemaTag2 `tony:"schema=other"`
		Name       string
	}

	type PersonWithComment struct {
		schemaTag `tony:"schema=person,comment=Comments"`
		Name      string
		Comments  []string
	}

	type PersonWithLineComment struct {
		schemaTag    `tony:"schema=person,lineComment=LineComments"`
		Name         string
		LineComments []string
	}

	type PersonWithBothComments struct {
		schemaTag    `tony:"schema=person,comment=Comments,lineComment=LineComments"`
		Name         string
		Comments     []string
		LineComments []string
	}

	type PersonWithQuotedSchema struct {
		schemaTag `tony:"schema='person schema'"`
		Name      string
	}

	type PersonWithTag struct {
		schemaTag `tony:"schema=person,tag=Tag"`
		Name      string
		Tag       string
	}

	type PersonWithTagAndComments struct {
		schemaTag    `tony:"schema=person,tag=Tag,comment=Comments,lineComment=LineComments"`
		Name         string
		Tag          string
		Comments     []string
		LineComments []string
	}

	// Nested embedded structs: B embeds A, both have schema tags
	type A struct {
		schemaTag `tony:"schema=person"`
		Name      string
	}

	type B struct {
		schemaTag `tony:"schema=company"`
		A
		CompanyName string
	}

	tests := []struct {
		name    string
		typ     reflect.Type
		want    *StructSchema
		wantErr bool
	}{
		{
			name: "schema mode",
			typ:  reflect.TypeOf(Person{}),
			want: &StructSchema{
				Mode:       "schema",
				SchemaName: "person",
				AllowExtra: false,
			},
		},
		{
			name: "schemagen mode",
			typ:  reflect.TypeOf(PersonDef{}),
			want: &StructSchema{
				Mode:       "schemagen",
				SchemaName: "person",
				AllowExtra: false,
			},
		},
		{
			name: "with allowExtra",
			typ:  reflect.TypeOf(PersonWithExtra{}),
			want: &StructSchema{
				Mode:       "schema",
				SchemaName: "person",
				AllowExtra: true,
			},
		},
		{
			name:    "no schema tag",
			typ:     reflect.TypeOf(NoSchema{}),
			wantErr: true,
		},
		{
			name:    "multiple schema tags",
			typ:     reflect.TypeOf(MultipleSchemas{}),
			wantErr: true,
		},
		{
			name: "with comment field",
			typ:  reflect.TypeOf(PersonWithComment{}),
			want: &StructSchema{
				Mode:             "schema",
				SchemaName:       "person",
				AllowExtra:       false,
				CommentFieldName: "Comments",
			},
		},
		{
			name: "with line comment field",
			typ:  reflect.TypeOf(PersonWithLineComment{}),
			want: &StructSchema{
				Mode:                 "schema",
				SchemaName:           "person",
				AllowExtra:           false,
				LineCommentFieldName: "LineComments",
			},
		},
		{
			name: "with both comment fields",
			typ:  reflect.TypeOf(PersonWithBothComments{}),
			want: &StructSchema{
				Mode:                 "schema",
				SchemaName:           "person",
				AllowExtra:           false,
				CommentFieldName:     "Comments",
				LineCommentFieldName: "LineComments",
			},
		},
		{
			name: "quoted schema name with spaces",
			typ:  reflect.TypeOf(PersonWithQuotedSchema{}),
			want: &StructSchema{
				Mode:       "schema",
				SchemaName: "person schema",
				AllowExtra: false,
			},
		},
		{
			name: "with tag field",
			typ:  reflect.TypeOf(PersonWithTag{}),
			want: &StructSchema{
				Mode:         "schema",
				SchemaName:   "person",
				AllowExtra:   false,
				TagFieldName: "Tag",
			},
		},
		{
			name: "with tag field and comments",
			typ:  reflect.TypeOf(PersonWithTagAndComments{}),
			want: &StructSchema{
				Mode:                 "schema",
				SchemaName:           "person",
				AllowExtra:           false,
				TagFieldName:         "Tag",
				CommentFieldName:     "Comments",
				LineCommentFieldName: "LineComments",
			},
		},
		{
			name: "nested embedded struct with schema tag",
			typ:  reflect.TypeOf(B{}),
			want: &StructSchema{
				Mode:       "schema",
				SchemaName: "company",
				AllowExtra: false,
			},
			// Note: B's own schema tag takes precedence. A's schema tag is ignored
			// because GetStructSchema only looks at direct anonymous fields, not
			// recursively into embedded structs.
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetStructSchema(tt.typ)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetStructSchema() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if got.Mode != tt.want.Mode {
				t.Errorf("GetStructSchema().Mode = %q, want %q", got.Mode, tt.want.Mode)
			}
			if got.SchemaName != tt.want.SchemaName {
				t.Errorf("GetStructSchema().SchemaName = %q, want %q", got.SchemaName, tt.want.SchemaName)
			}
			if got.AllowExtra != tt.want.AllowExtra {
				t.Errorf("GetStructSchema().AllowExtra = %v, want %v", got.AllowExtra, tt.want.AllowExtra)
			}
			if got.CommentFieldName != tt.want.CommentFieldName {
				t.Errorf("GetStructSchema().CommentFieldName = %q, want %q", got.CommentFieldName, tt.want.CommentFieldName)
			}
			if got.LineCommentFieldName != tt.want.LineCommentFieldName {
				t.Errorf("GetStructSchema().LineCommentFieldName = %q, want %q", got.LineCommentFieldName, tt.want.LineCommentFieldName)
			}
			if got.TagFieldName != tt.want.TagFieldName {
				t.Errorf("GetStructSchema().TagFieldName = %q, want %q", got.TagFieldName, tt.want.TagFieldName)
			}
		})
	}
}

func TestGetStructFields_SchemadefMode(t *testing.T) {
	type Person struct {
		schemaTag `tony:"schemagen=person"`
		Name      string
		Age       int
		Email     *string `tony:"required"`
		Tags      []string
		Notes     string `tony:"optional"`
		Internal  string `tony:"omit"`
		Custom    string `tony:"field=name"`
	}

	typ := reflect.TypeOf(Person{})
	fields, err := GetStructFields(typ, nil, "schemagen", false, nil)
	if err != nil {
		t.Fatalf("GetStructFields() error = %v", err)
	}

	// Should have 6 fields (excluding Internal which is omitted)
	if len(fields) != 6 {
		t.Errorf("GetStructFields() returned %d fields, want 6", len(fields))
	}

	// Check Name field
	nameField := findField(fields, "Name")
	if nameField == nil {
		t.Fatal("Name field not found")
	}
	if nameField.SchemaFieldName != "Name" {
		t.Errorf("Name.SchemaFieldName = %q, want %q", nameField.SchemaFieldName, "Name")
	}
	if nameField.Optional {
		t.Error("Name should not be optional (non-pointer, non-slice)")
	}

	// Check Email field (pointer with required tag)
	emailField := findField(fields, "Email")
	if emailField == nil {
		t.Fatal("Email field not found")
	}
	if !emailField.Required {
		t.Error("Email should be required (has required tag)")
	}
	if emailField.Optional {
		t.Error("Email should not be optional (has required tag)")
	}

	// Check Tags field (slice)
	tagsField := findField(fields, "Tags")
	if tagsField == nil {
		t.Fatal("Tags field not found")
	}
	if !tagsField.Optional {
		t.Error("Tags should be optional (slice type)")
	}

	// Check Notes field (non-pointer with optional tag)
	notesField := findField(fields, "Notes")
	if notesField == nil {
		t.Fatal("Notes field not found")
	}
	if !notesField.Optional {
		t.Error("Notes should be optional (has optional tag)")
	}

	// Check Custom field (field name override)
	customField := findField(fields, "Custom")
	if customField == nil {
		t.Fatal("Custom field not found")
	}
	if customField.SchemaFieldName != "name" {
		t.Errorf("Custom.SchemaFieldName = %q, want %q", customField.SchemaFieldName, "name")
	}

	// Verify Internal is omitted
	if findField(fields, "Internal") != nil {
		t.Error("Internal field should be omitted")
	}
}

func TestGetStructFields_RequiredAndOptionalConflict(t *testing.T) {
	type Person struct {
		schemaTag `tony:"schemagen=person"`
		Name      string `tony:"required,optional"`
	}

	typ := reflect.TypeOf(Person{})
	_, err := GetStructFields(typ, nil, "schemagen", false, nil)
	if err == nil {
		t.Error("GetStructFields() should error when both required and optional tags are present")
	}
}

func TestGetStructFields_EmbeddedStructs(t *testing.T) {
	// A has a schema tag
	type A struct {
		schemaTag `tony:"schema=person"`
		Name      string
		Age       int
	}

	// B embeds A, both have schema tags (A should be nested, not flattened)
	type B struct {
		schemaTag `tony:"schema=company"`
		A
		CompanyName string
	}

	// C embeds A, only A has schema tag (A should NOT be flattened, treated as nested object)
	type C struct {
		A
		ExtraField string
	}

	// E embeds a struct without schema tag (fields should be flattened)
	type E struct {
		Name string
		Age  int
	}
	type F struct {
		E
		ExtraField string
	}

	// D embeds A, only D has schema tag
	// Note: In schemagen mode, A's fields ARE flattened (we're generating a schema definition)
	// In schema mode, A would NOT be flattened (treated as nested object)
	type D struct {
		schemaTag   `tony:"schema=company"`
		A           // A has schema tag, but flattened in schemagen mode
		CompanyName string
	}

	t.Run("embedded struct with schema tag should be flattened in schemagen mode", func(t *testing.T) {
		typ := reflect.TypeOf(C{})
		fields, err := GetStructFields(typ, nil, "schemagen", false, nil)
		if err != nil {
			t.Fatalf("GetStructFields() error = %v", err)
		}

		// In schemagen mode, embedded structs are flattened regardless of schema tags
		// (we're generating a schema definition, so include all fields)
		if len(fields) != 3 {
			t.Errorf("GetStructFields() returned %d fields, want 3 (A's fields should be flattened)", len(fields))
		}

		if findField(fields, "Name") == nil {
			t.Error("Name field from embedded A should be flattened in schemagen mode")
		}
		if findField(fields, "Age") == nil {
			t.Error("Age field from embedded A should be flattened in schemagen mode")
		}
		if findField(fields, "ExtraField") == nil {
			t.Error("ExtraField should be present")
		}
	})

	t.Run("embedded struct without schema tag should be flattened in schemagen mode", func(t *testing.T) {
		typ := reflect.TypeOf(F{})
		fields, err := GetStructFields(typ, nil, "schemagen", false, nil)
		if err != nil {
			t.Fatalf("GetStructFields() error = %v", err)
		}

		// Should have Name, Age (from E, flattened), and ExtraField
		if len(fields) != 3 {
			t.Errorf("GetStructFields() returned %d fields, want 3", len(fields))
		}

		if findField(fields, "Name") == nil {
			t.Error("Name field from embedded E should be flattened")
		}
		if findField(fields, "Age") == nil {
			t.Error("Age field from embedded E should be flattened")
		}
		if findField(fields, "ExtraField") == nil {
			t.Error("ExtraField should be present")
		}
	})

	t.Run("embedded struct with schema tag in parent with schema tag", func(t *testing.T) {
		typ := reflect.TypeOf(D{})
		fields, err := GetStructFields(typ, nil, "schemagen", false, nil)
		if err != nil {
			t.Fatalf("GetStructFields() error = %v", err)
		}

		// In schemagen mode, embedded structs are flattened regardless of schema tags
		if len(fields) != 3 {
			t.Errorf("GetStructFields() returned %d fields, want 3 (A's fields should be flattened)", len(fields))
		}

		if findField(fields, "Name") == nil {
			t.Error("Name field from embedded A should be flattened in schemagen mode")
		}
		if findField(fields, "Age") == nil {
			t.Error("Age field from embedded A should be flattened in schemagen mode")
		}
		if findField(fields, "CompanyName") == nil {
			t.Error("CompanyName should be present")
		}
	})

	t.Run("embedded struct with schema tag should be flattened in schemagen mode", func(t *testing.T) {
		typ := reflect.TypeOf(B{})
		fields, err := GetStructFields(typ, nil, "schemagen", false, nil)
		if err != nil {
			t.Fatalf("GetStructFields() error = %v", err)
		}

		// In schemagen mode, embedded structs are flattened regardless of schema tags
		// (we're generating a schema definition, so include all fields)
		if len(fields) != 3 {
			t.Errorf("GetStructFields() returned %d fields, want 3 (A's fields should be flattened)", len(fields))
		}

		if findField(fields, "Name") == nil {
			t.Error("Name field from embedded A should be flattened in schemagen mode")
		}
		if findField(fields, "Age") == nil {
			t.Error("Age field from embedded A should be flattened in schemagen mode")
		}
		if findField(fields, "CompanyName") == nil {
			t.Error("CompanyName should be present")
		}
	})
}

func TestGetStructFields_SchemaMode_EmbeddedStructValidation(t *testing.T) {
	// Test that embedded struct fields are validated against the schema in schema= mode

	// Create a schema that expects Name (string), Age (int), CompanyName (string)
	companySchema := &schema.Schema{
		Accept: &ir.Node{
			Type: ir.ObjectType,
			Fields: []*ir.Node{
				{Type: ir.StringType, String: "Name"},
				{Type: ir.StringType, String: "Age"},
				{Type: ir.StringType, String: "CompanyName"},
			},
			Values: []*ir.Node{
				{Type: ir.StringType}, // Name: string
				{Type: ir.NumberType}, // Age: int
				{Type: ir.StringType}, // CompanyName: string
			},
		},
	}

	// Person struct with schema tag
	type Person struct {
		schemaTag `tony:"schema=person"`
		Name      string
		Age       float64 // NumberType maps to float64
	}

	// Company struct that embeds Person - should have Name, Age (from Person), CompanyName
	type Company struct {
		schemaTag `tony:"schema=company"`
		Person
		CompanyName string
	}

	// CompanyWithExtra embeds Person but has an extra field not in schema
	type CompanyWithExtra struct {
		schemaTag `tony:"schema=company"`
		Person
		CompanyName string
		ExtraField  string // Not in schema
	}

	// CompanyMissingField embeds Person but is missing CompanyName
	type CompanyMissingField struct {
		schemaTag `tony:"schema=company"`
		Person
		// Missing CompanyName
	}

	t.Run("embedded struct fields match schema", func(t *testing.T) {
		typ := reflect.TypeOf(Company{})
		fields, err := GetStructFields(typ, companySchema, "schema", false, nil)
		if err != nil {
			t.Fatalf("GetStructFields() error = %v", err)
		}

		// Should have Name, Age (from Person), CompanyName
		if len(fields) != 3 {
			t.Errorf("GetStructFields() returned %d fields, want 3", len(fields))
		}

		if findField(fields, "Name") == nil {
			t.Error("Name field from embedded Person should be found")
		}
		if findField(fields, "Age") == nil {
			t.Error("Age field from embedded Person should be found")
		}
		if findField(fields, "CompanyName") == nil {
			t.Error("CompanyName should be found")
		}
	})

	t.Run("embedded struct has extra field not in schema", func(t *testing.T) {
		typ := reflect.TypeOf(CompanyWithExtra{})
		_, err := GetStructFields(typ, companySchema, "schema", false, nil)
		if err == nil {
			t.Error("GetStructFields() should error when embedded struct has extra field not in schema")
		}
		if err != nil && !contains(err.Error(), "extra field") {
			t.Errorf("GetStructFields() error = %v, want error about extra field", err)
		}
	})

	t.Run("embedded struct missing required field from schema", func(t *testing.T) {
		typ := reflect.TypeOf(CompanyMissingField{})
		_, err := GetStructFields(typ, companySchema, "schema", false, nil)
		if err == nil {
			t.Error("GetStructFields() should error when struct is missing required field from schema")
		}
		if err != nil && !contains(err.Error(), "not found") {
			t.Errorf("GetStructFields() error = %v, want error about missing field", err)
		}
	})

	t.Run("embedded struct with allowExtra flag", func(t *testing.T) {
		type CompanyWithAllowExtra struct {
			schemaTag `tony:"schema=company,allowExtra"`
			Person
			CompanyName string
			ExtraField  string // Not in schema, but allowExtra is set
		}

		typ := reflect.TypeOf(CompanyWithAllowExtra{})
		fields, err := GetStructFields(typ, companySchema, "schema", true, nil)
		if err != nil {
			t.Fatalf("GetStructFields() error = %v", err)
		}

		// Should have Name, Age, CompanyName (ExtraField is allowed but not in schema fields)
		if len(fields) != 3 {
			t.Errorf("GetStructFields() returned %d fields, want 3", len(fields))
		}
	})
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func findField(fields []*FieldInfo, name string) *FieldInfo {
	for _, f := range fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func TestGetStructFields_SchemaMode_FieldRenaming(t *testing.T) {
	// Test that field= tags work in schema= mode to rename struct fields
	// when matching to schema fields

	// Create a schema that expects "name" (lowercase) and "age"
	personSchema := &schema.Schema{
		Accept: &ir.Node{
			Type: ir.ObjectType,
			Fields: []*ir.Node{
				{Type: ir.StringType, String: "name"},
				{Type: ir.StringType, String: "age"},
			},
			Values: []*ir.Node{
				{Type: ir.StringType}, // name: string
				{Type: ir.NumberType}, // age: float64 (NumberType maps to float64)
			},
		},
	}

	// Person struct with schema tag and field renaming
	type Person struct {
		schemaTag `tony:"schema=person"`
		FullName  string  `tony:"field=name"` // Struct field "FullName" maps to schema field "name"
		Age       float64 `tony:"field=age"`  // Struct field "Age" maps to schema field "age"
	}

	typ := reflect.TypeOf(Person{})
	fields, err := GetStructFields(typ, personSchema, "schema", false, nil)
	if err != nil {
		t.Fatalf("failed to get struct fields: %v", err)
	}

	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}

	// Check first field (name)
	nameField := fields[0]
	if nameField.Name != "FullName" {
		t.Errorf("expected field name 'FullName', got %q", nameField.Name)
	}
	if nameField.SchemaFieldName != "name" {
		t.Errorf("expected schema field name 'name', got %q", nameField.SchemaFieldName)
	}
	if nameField.Type != reflect.TypeOf("") {
		t.Errorf("expected type string, got %v", nameField.Type)
	}

	// Check second field (age)
	ageField := fields[1]
	if ageField.Name != "Age" {
		t.Errorf("expected field name 'Age', got %q", ageField.Name)
	}
	if ageField.SchemaFieldName != "age" {
		t.Errorf("expected schema field name 'age', got %q", ageField.SchemaFieldName)
	}
	if ageField.Type != reflect.TypeOf(float64(0)) {
		t.Errorf("expected type float64, got %v", ageField.Type)
	}
}

func TestGetStructFields_SchemaMode_FieldRenaming_Embedded(t *testing.T) {
	// Test that field= tags work in schema= mode with embedded structs

	// Create a schema that expects "first_name" and "last_name"
	nameSchema := &schema.Schema{
		Accept: &ir.Node{
			Type: ir.ObjectType,
			Fields: []*ir.Node{
				{Type: ir.StringType, String: "first_name"},
				{Type: ir.StringType, String: "last_name"},
			},
			Values: []*ir.Node{
				{Type: ir.StringType}, // first_name: string
				{Type: ir.StringType}, // last_name: string
			},
		},
	}

	// Name struct with field renaming
	type Name struct {
		FirstName string `tony:"field=first_name"`
		LastName  string `tony:"field=last_name"`
	}

	// Person struct that embeds Name
	type Person struct {
		schemaTag `tony:"schema=person"`
		Name
	}

	typ := reflect.TypeOf(Person{})
	fields, err := GetStructFields(typ, nameSchema, "schema", false, nil)
	if err != nil {
		t.Fatalf("failed to get struct fields: %v", err)
	}

	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}

	// Check first field (first_name)
	firstNameField := fields[0]
	if firstNameField.Name != "FirstName" {
		t.Errorf("expected field name 'FirstName', got %q", firstNameField.Name)
	}
	if firstNameField.SchemaFieldName != "first_name" {
		t.Errorf("expected schema field name 'first_name', got %q", firstNameField.SchemaFieldName)
	}

	// Check second field (last_name)
	lastNameField := fields[1]
	if lastNameField.Name != "LastName" {
		t.Errorf("expected field name 'LastName', got %q", lastNameField.Name)
	}
	if lastNameField.SchemaFieldName != "last_name" {
		t.Errorf("expected schema field name 'last_name', got %q", lastNameField.SchemaFieldName)
	}
}
