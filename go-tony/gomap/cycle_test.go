package gomap

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestCircularReference_Marshal(t *testing.T) {
	type Person struct {
		Name string
		Boss *Person
	}

	person := &Person{Name: "Alice"}
	person.Boss = person // Circular reference!

	_, err := ToIR(person)
	if err == nil {
		t.Fatal("expected error for circular reference")
	}

	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("expected error message to contain 'circular', got: %v", err)
	}
}

func TestCircularReference_Unmarshal(t *testing.T) {
	type Person struct {
		Name string
		Boss *Person
	}

	// Test that unmarshaling a valid structure works
	// Create a simple object (no cycle)
	node := ir.FromMap(map[string]*ir.Node{
		"Name": ir.FromString("Alice"),
		"Boss": ir.FromMap(map[string]*ir.Node{
			"Name": ir.FromString("Bob"),
		}),
	})

	var person Person
	err := FromIR(node, &person)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if person.Name != "Alice" {
		t.Errorf("expected Name='Alice', got %q", person.Name)
	}
	if person.Boss == nil {
		t.Fatal("expected Boss to be set")
	}
	if person.Boss.Name != "Bob" {
		t.Errorf("expected Boss.Name='Bob', got %q", person.Boss.Name)
	}
}

func TestCircularReference_StructSlice(t *testing.T) {
	type Person struct {
		Name    string
		Reports []*Person // Slice of pointers to create actual cycles
	}

	person := &Person{Name: "Alice"}
	person.Reports = []*Person{person} // Circular reference via slice of pointers!

	_, err := ToIR(person)
	if err == nil {
		t.Fatal("expected error for circular reference")
	}

	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("expected error message to contain 'circular', got: %v", err)
	}
}

func TestCircularReference_StructMap(t *testing.T) {
	type Person struct {
		Name   string
		Peers  map[string]*Person
	}

	person := &Person{Name: "Alice", Peers: make(map[string]*Person)}
	person.Peers["self"] = person // Circular reference via map!

	_, err := ToIR(person)
	if err == nil {
		t.Fatal("expected error for circular reference")
	}

	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("expected error message to contain 'circular', got: %v", err)
	}
}

func TestCircularReference_NoCycle(t *testing.T) {
	type Person struct {
		Name string
		Boss *Person
	}

	alice := &Person{Name: "Alice"}
	bob := &Person{Name: "Bob", Boss: alice}
	// No cycle - Bob points to Alice, but Alice doesn't point back

	node, err := ToIR(bob)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if node == nil {
		t.Fatal("expected non-nil node")
	}

	// Unmarshal back
	var result Person
	err = FromIR(node, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Name != "Bob" {
		t.Errorf("expected Name='Bob', got %q", result.Name)
	}
	if result.Boss == nil {
		t.Fatal("expected Boss to be set")
	}
	if result.Boss.Name != "Alice" {
		t.Errorf("expected Boss.Name='Alice', got %q", result.Boss.Name)
	}
}

func TestCircularReference_NestedStruct(t *testing.T) {
	// Define types with forward reference using interface{} first, then convert
	type Person struct {
		Name    string
		Address interface{} // Will be *Address
	}
	type Address struct {
		Street string
		Owner  *Person
	}

	person := &Person{Name: "Alice"}
	address := &Address{Street: "123 Main St", Owner: person}
	person.Address = address // Circular reference!

	_, err := ToIR(person)
	if err == nil {
		t.Fatal("expected error for circular reference")
	}

	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("expected error message to contain 'circular', got: %v", err)
	}
}
