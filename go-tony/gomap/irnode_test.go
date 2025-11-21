package gomap_test

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
)

// TestIRNodeField tests that *ir.Node fields are properly handled via reflection.
func TestIRNodeField(t *testing.T) {
	type RequestBody struct {
		Path  *ir.Node `tony:"field=path"`
		Match *ir.Node `tony:"field=match"`
		Patch *ir.Node `tony:"field=patch"`
		Meta  *ir.Node `tony:"field=meta"`
	}

	// Create test data
	original := &RequestBody{
		Path: ir.FromString("/users/123"),
		Match: ir.FromMap(map[string]*ir.Node{
			"status": ir.FromString("active"),
		}),
		Patch: ir.FromMap(map[string]*ir.Node{
			"name": ir.FromString("John"),
		}),
		Meta: ir.Null(),
	}

	// Convert to IR
	node, err := gomap.ToTonyIR(original)
	if err != nil {
		t.Fatalf("ToTonyIR failed: %v", err)
	}

	// Verify structure
	if node.Type != ir.ObjectType {
		t.Fatalf("Expected ObjectType, got %v", node.Type)
	}

	t.Logf("Generated node has %d fields", len(node.Fields))
	for i, field := range node.Fields {
		t.Logf("Field %d: %s = %v (type %v)", i, field.String, node.Values[i], node.Values[i].Type)
	}

	irMap := ir.ToMap(node)
	if irMap["path"] == nil {
		t.Fatal("path field is missing")
	}
	if irMap["path"].String != "/users/123" {
		t.Errorf("path = %q, want %q", irMap["path"].String, "/users/123")
	}

	// Convert back from IR
	result := &RequestBody{}
	if err := gomap.FromTonyIR(node, result); err != nil {
		t.Fatalf("FromTonyIR failed: %v", err)
	}

	// Verify round-trip
	if result.Path == nil || result.Path.String != "/users/123" {
		t.Errorf("Path not preserved: got %v", result.Path)
	}
	if result.Match == nil {
		t.Fatal("Match is nil")
	}
	matchMap := ir.ToMap(result.Match)
	if matchMap["status"] == nil || matchMap["status"].String != "active" {
		t.Errorf("Match.status not preserved")
	}
	if result.Patch == nil {
		t.Fatal("Patch is nil")
	}
	patchMap := ir.ToMap(result.Patch)
	if patchMap["name"] == nil || patchMap["name"].String != "John" {
		t.Errorf("Patch.name not preserved")
	}
	if result.Meta == nil || result.Meta.Type != ir.NullType {
		t.Errorf("Meta should be null, got %v", result.Meta)
	}
}

// TestIRNodeFieldNil tests that nil *ir.Node fields are handled correctly.
func TestIRNodeFieldNil(t *testing.T) {
	type Container struct {
		Data *ir.Node `tony:"field=data"`
	}

	// Test nil field
	original := &Container{
		Data: nil,
	}

	node, err := gomap.ToTonyIR(original)
	if err != nil {
		t.Fatalf("ToTonyIR failed: %v", err)
	}

	irMap := ir.ToMap(node)
	if irMap["data"] == nil || irMap["data"].Type != ir.NullType {
		t.Errorf("Expected null for nil *ir.Node field, got %v", irMap["data"])
	}

	// Convert back
	result := &Container{}
	if err := gomap.FromTonyIR(node, result); err != nil {
		t.Fatalf("FromTonyIR failed: %v", err)
	}

	if result.Data != nil && result.Data.Type != ir.NullType {
		t.Errorf("Expected nil or null for Data, got %v", result.Data)
	}
}
