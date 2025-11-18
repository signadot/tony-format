package gomap

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
)

func TestMapper_ToIR_WithSchema(t *testing.T) {
	// Create a simple schema
	ctxReg := schema.NewContextRegistry()
	schemaReg := schema.NewSchemaRegistry(ctxReg)

	personSchema := &schema.Schema{
		Context: schema.DefaultContext(),
		Signature: &schema.Signature{
			Name: "person",
		},
		Accept: ir.FromMap(map[string]*ir.Node{
			"Name": ir.FromString(""),
			"Age":  ir.FromInt(0),
		}),
	}

	err := schemaReg.RegisterSchema(personSchema)
	if err != nil {
		t.Fatalf("failed to register schema: %v", err)
	}

	mapper := NewMapper(schemaReg, ctxReg)

	t.Run("schema-aware marshaling", func(t *testing.T) {
		type Person struct {
			schemaTag `tony:"schema=person"`
			Name      string
			Age       int
		}

		person := Person{Name: "Alice", Age: 30}
		node, err := mapper.ToIR(person)
		if err != nil {
			t.Fatalf("ToIR() error = %v", err)
		}

		if node.Tag != "!person" {
			t.Errorf("node.Tag = %q, want !person", node.Tag)
		}

		if node.Type != ir.ObjectType {
			t.Errorf("node.Type = %v, want ObjectType", node.Type)
		}

		// Check that fields use schema field names
		if len(node.Fields) != 2 {
			t.Errorf("node.Fields length = %d, want 2", len(node.Fields))
		}

		fieldMap := make(map[string]*ir.Node)
		for i, fieldNameNode := range node.Fields {
			if i < len(node.Values) {
				fieldMap[fieldNameNode.String] = node.Values[i]
			}
		}

		if nameNode, ok := fieldMap["Name"]; !ok || nameNode.String != "Alice" {
			t.Errorf("Name field = %v, want Alice", nameNode)
		}
		if ageNode, ok := fieldMap["Age"]; !ok || ageNode.Int64 == nil || *ageNode.Int64 != 30 {
			t.Errorf("Age field = %v, want 30", ageNode)
		}
	})

	t.Run("fallback to reflection without schema tag", func(t *testing.T) {
		type Person struct {
			Name string
			Age  int
		}

		person := Person{Name: "Bob", Age: 25}
		// Use the package-level ToIR function (not mapper) for reflection-only mode
		node, err := ToIR(person)
		if err != nil {
			t.Fatalf("ToIR() error = %v", err)
		}

		// Should not have schema tag
		if node.Tag != "" {
			t.Errorf("node.Tag = %q, want empty (no schema tag)", node.Tag)
		}

		// Should use Go field names (reflection mode)
		fieldMap := make(map[string]*ir.Node)
		for i, fieldNameNode := range node.Fields {
			if i < len(node.Values) {
				fieldMap[fieldNameNode.String] = node.Values[i]
			}
		}

		if nameNode, ok := fieldMap["Name"]; !ok || nameNode.String != "Bob" {
			t.Errorf("Name field = %v, want Bob (Go field name)", nameNode)
		}
	})
}

func TestMapper_FromIR_WithSchema(t *testing.T) {
	// Create a simple schema
	ctxReg := schema.NewContextRegistry()
	schemaReg := schema.NewSchemaRegistry(ctxReg)

	personSchema := &schema.Schema{
		Context: schema.DefaultContext(),
		Signature: &schema.Signature{
			Name: "person",
		},
		Accept: ir.FromMap(map[string]*ir.Node{
			"Name": ir.FromString(""),
			"Age":  ir.FromInt(0),
		}),
	}

	err := schemaReg.RegisterSchema(personSchema)
	if err != nil {
		t.Fatalf("failed to register schema: %v", err)
	}

	mapper := NewMapper(schemaReg, ctxReg)

	t.Run("schema-aware unmarshaling", func(t *testing.T) {
		type Person struct {
			schemaTag `tony:"schema=person"`
			Name      string
			Age       int
		}

		node := ir.FromMap(map[string]*ir.Node{
			"Name": ir.FromString("Charlie"),
			"Age":  ir.FromInt(35),
		})
		node.Tag = "!person"

		var person Person
		err := mapper.FromIR(node, &person)
		if err != nil {
			t.Fatalf("FromIR() error = %v", err)
		}

		if person.Name != "Charlie" {
			t.Errorf("person.Name = %q, want Charlie", person.Name)
		}
		if person.Age != 35 {
			t.Errorf("person.Age = %d, want 35", person.Age)
		}
	})

	t.Run("required field validation", func(t *testing.T) {
		type Person struct {
			schemaTag `tony:"schema=person"`
			Name      string
			Age       int
		}

		// Missing required field "Age"
		node := ir.FromMap(map[string]*ir.Node{
			"Name": ir.FromString("David"),
		})
		node.Tag = "!person"

		var person Person
		err := mapper.FromIR(node, &person)
		if err == nil {
			t.Error("FromIR() should error on missing required field")
		}
	})

	t.Run("extra field validation", func(t *testing.T) {
		type Person struct {
			schemaTag `tony:"schema=person"` // no allowExtra
			Name      string
			Age       int
		}

		// Extra field "Email" not in schema
		node := ir.FromMap(map[string]*ir.Node{
			"Name":  ir.FromString("Eve"),
			"Age":   ir.FromInt(28),
			"Email": ir.FromString("eve@example.com"),
		})
		node.Tag = "!person"

		var person Person
		err := mapper.FromIR(node, &person)
		if err == nil {
			t.Error("FromIR() should error on extra field when allowExtra is false")
		}
	})

	t.Run("allowExtra flag", func(t *testing.T) {
		type Person struct {
			schemaTag `tony:"schema=person,allowExtra"`
			Name      string
			Age       int
		}

		// Extra field "Email" - should be allowed
		node := ir.FromMap(map[string]*ir.Node{
			"Name":  ir.FromString("Frank"),
			"Age":   ir.FromInt(32),
			"Email": ir.FromString("frank@example.com"),
		})
		node.Tag = "!person"

		var person Person
		err := mapper.FromIR(node, &person)
		if err != nil {
			t.Fatalf("FromIR() error = %v (should allow extra fields)", err)
		}

		if person.Name != "Frank" {
			t.Errorf("person.Name = %q, want Frank", person.Name)
		}
	})

	t.Run("fallback to reflection without schema tag", func(t *testing.T) {
		type Person struct {
			Name string
			Age  int
		}

		node := ir.FromMap(map[string]*ir.Node{
			"Name": ir.FromString("Grace"), // Go field name
			"Age":  ir.FromInt(27),
		})

		var person Person
		// Use the package-level FromIR function (not mapper) for reflection-only mode
		err := FromIR(node, &person)
		if err != nil {
			t.Fatalf("FromIR() error = %v", err)
		}

		if person.Name != "Grace" {
			t.Errorf("person.Name = %q, want Grace", person.Name)
		}
	})
}

func TestMapper_ToIR_OptionalFields(t *testing.T) {
	ctxReg := schema.NewContextRegistry()
	schemaReg := schema.NewSchemaRegistry(ctxReg)

	// Schema with optional field
	// Create a nullable int node for optional age field
	// Use ir.FromInt(0) to create a node that will infer int64 type
	ageNode := ir.FromInt(0)
	personSchema := &schema.Schema{
		Context: schema.DefaultContext(),
		Signature: &schema.Signature{
			Name: "person",
		},
		Accept: ir.FromMap(map[string]*ir.Node{
			"Name": ir.FromString(""),
			"Age":  ageNode, // Optional (nullable - will be detected via pointer type)
		}),
	}

	err := schemaReg.RegisterSchema(personSchema)
	if err != nil {
		t.Fatalf("failed to register schema: %v", err)
	}

	mapper := NewMapper(schemaReg, ctxReg)

	t.Run("skip optional zero values", func(t *testing.T) {
		type Person struct {
			schemaTag `tony:"schema=person"`
			Name      string
			Age       *int // Optional
		}

		person := Person{Name: "Alice", Age: nil} // Age is zero value
		node, err := mapper.ToIR(person)
		if err != nil {
			t.Fatalf("ToIR() error = %v", err)
		}

		fieldMap := make(map[string]*ir.Node)
		for i, fieldNameNode := range node.Fields {
			if i < len(node.Values) {
				fieldMap[fieldNameNode.String] = node.Values[i]
			}
		}

		// Age should be omitted (zero value for optional field)
		if _, ok := fieldMap["Age"]; ok {
			t.Error("Age field should be omitted when zero value")
		}

		if nameNode, ok := fieldMap["Name"]; !ok || nameNode.String != "Alice" {
			t.Errorf("Name field = %v, want Alice", nameNode)
		}
	})

	t.Run("include optional non-zero values", func(t *testing.T) {
		type Person struct {
			schemaTag `tony:"schema=person"`
			Name      string
			Age       *int
		}

		age := 30
		person := Person{Name: "Bob", Age: &age}
		node, err := mapper.ToIR(person)
		if err != nil {
			t.Fatalf("ToIR() error = %v", err)
		}

		fieldMap := make(map[string]*ir.Node)
		for i, fieldNameNode := range node.Fields {
			if i < len(node.Values) {
				fieldMap[fieldNameNode.String] = node.Values[i]
			}
		}

		// Age should be included (non-zero value)
		if ageNodeVal, ok := fieldMap["Age"]; !ok || ageNodeVal.Int64 == nil || *ageNodeVal.Int64 != 30 {
			t.Errorf("Age field = %v, want 30", ageNodeVal)
		}
	})
}

func TestMapper_DefaultRegistries(t *testing.T) {
	ctxReg := schema.NewContextRegistry()
	schemaReg := schema.NewSchemaRegistry(ctxReg)

	// Set default registries
	SetDefaultRegistries(schemaReg, ctxReg)

	// Create mapper with nil registries - should use defaults
	mapper := NewMapper(nil, nil)

	if mapper.SchemaRegistry() != schemaReg {
		t.Error("Mapper should use default schema registry")
	}
	if mapper.ContextRegistry() != ctxReg {
		t.Error("Mapper should use default context registry")
	}

	// Clean up
	SetDefaultRegistries(nil, nil)
}

func TestIsZeroValue(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want bool
	}{
		{"zero int", 0, true},
		{"non-zero int", 42, false},
		{"zero string", "", true},
		{"non-zero string", "hello", false},
		{"zero bool", false, true},
		{"non-zero bool", true, false},
		{"nil pointer", (*int)(nil), true},
		{"non-nil pointer", intPtr(42), false},
		{"nil slice", []string(nil), true},
		{"empty slice", []string{}, false}, // Empty slice is not zero value
		{"nil map", map[string]int(nil), true},
		{"empty map", map[string]int{}, false}, // Empty map is not zero value
		{"zero struct", struct{}{}, true},
		{"struct with zero fields", struct{ Name string }{}, true},
		{"struct with non-zero field", struct{ Name string }{"Alice"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.val)
			got := isZeroValue(val)
			if got != tt.want {
				t.Errorf("isZeroValue(%v) = %v, want %v", tt.val, got, tt.want)
			}
		})
	}
}

// Helper function for tests
func intPtrMapper(i int) *int {
	return &i
}
