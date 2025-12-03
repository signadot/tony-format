package tx

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Helper to get a map from an object node
func getMap(node *ir.Node) map[string]*ir.Node {
	if node == nil || node.Type != ir.ObjectType {
		return nil
	}
	return ir.ToMap(node)
}

// Helper to get an int keys map from a node
func getIntKeysMap(node *ir.Node) (map[uint32]*ir.Node, error) {
	if node == nil || node.Type != ir.ObjectType {
		return nil, nil
	}
	return node.ToIntKeysMap()
}

// Helper to create a PatcherData for testing
func makePatcherData(path string, data *ir.Node) *PatcherData {
	return &PatcherData{
		ReceivedAt: time.Now(),
		API: &api.Patch{
			Patch: api.Body{
				Path: path,
				Data: data,
			},
		},
	}
}

func TestMergePatches_Empty(t *testing.T) {
	result, err := MergePatches(nil)
	if err != nil {
		t.Fatalf("MergePatches(nil) returned error: %v", err)
	}
	if result != nil {
		t.Errorf("MergePatches(nil) should return nil, got %v", result)
	}

	result, err = MergePatches([]*PatcherData{})
	if err != nil {
		t.Fatalf("MergePatches([]) returned error: %v", err)
	}
	if result != nil {
		t.Errorf("MergePatches([]) should return nil, got %v", result)
	}
}

func TestMergePatches_SinglePatch(t *testing.T) {
	data := ir.FromString("value")
	patch := makePatcherData("a.b", data)

	result, err := MergePatches([]*PatcherData{patch})
	if err != nil {
		t.Fatalf("MergePatches returned error: %v", err)
	}
	if result == nil {
		t.Fatal("MergePatches returned nil")
	}

	// Should create nested structure: {a: {b: "value"}}
	if result.Type != ir.ObjectType {
		t.Fatalf("Expected ObjectType, got %v", result.Type)
	}
	resultMap := getMap(result)
	if resultMap == nil {
		t.Fatal("Expected result to be a map")
	}

	aNode, ok := resultMap["a"]
	if !ok {
		t.Fatal("Expected key 'a' in result")
	}
	if aNode.Type != ir.ObjectType {
		t.Fatalf("Expected 'a' to be ObjectType, got %v", aNode.Type)
	}

	aMap := getMap(aNode)
	bNode, ok := aMap["b"]
	if !ok {
		t.Fatal("Expected key 'b' in 'a'")
	}
	if bNode.Type != ir.StringType || bNode.String != "value" {
		t.Fatalf("Expected 'b' to be string 'value', got %v", bNode)
	}
}

func TestMergePatches_MultiplePaths_SameParent(t *testing.T) {
	patch1 := makePatcherData("a.b", ir.FromString("value1"))
	patch2 := makePatcherData("a.c", ir.FromString("value2"))

	result, err := MergePatches([]*PatcherData{patch1, patch2})
	if err != nil {
		t.Fatalf("MergePatches returned error: %v", err)
	}

	// Should create: {a: {b: "value1", c: "value2"}}
	if result.Type != ir.ObjectType {
		t.Fatalf("Expected ObjectType, got %v", result.Type)
	}

	resultMap := getMap(result)
	aNode := resultMap["a"]
	if aNode.Type != ir.ObjectType {
		t.Fatalf("Expected 'a' to be ObjectType, got %v", aNode.Type)
	}

	aMap := getMap(aNode)
	if bNode := aMap["b"]; bNode.String != "value1" {
		t.Fatalf("Expected 'b' to be 'value1', got %v", bNode.String)
	}
	if cNode := aMap["c"]; cNode.String != "value2" {
		t.Fatalf("Expected 'c' to be 'value2', got %v", cNode.String)
	}
}

func TestMergePatches_MultiplePaths_DifferentRoots(t *testing.T) {
	patch1 := makePatcherData("a.b", ir.FromString("value1"))
	patch2 := makePatcherData("x.y", ir.FromString("value2"))

	result, err := MergePatches([]*PatcherData{patch1, patch2})
	if err != nil {
		t.Fatalf("MergePatches returned error: %v", err)
	}

	// Should create: {a: {b: "value1"}, x: {y: "value2"}}
	if result.Type != ir.ObjectType {
		t.Fatalf("Expected ObjectType, got %v", result.Type)
	}

	resultMap := getMap(result)
	aMap := getMap(resultMap["a"])
	if aMap["b"].String != "value1" {
		t.Fatalf("Expected a.b to be 'value1'")
	}

	xMap := getMap(resultMap["x"])
	if xMap["y"].String != "value2" {
		t.Fatalf("Expected x.y to be 'value2'")
	}
}

func TestMergePatches_NestedPaths(t *testing.T) {
	patch1 := makePatcherData("a.b.c", ir.FromString("value1"))
	patch2 := makePatcherData("a.b.d", ir.FromString("value2"))

	result, err := MergePatches([]*PatcherData{patch1, patch2})
	if err != nil {
		t.Fatalf("MergePatches returned error: %v", err)
	}

	// Should create: {a: {b: {c: "value1", d: "value2"}}}
	resultMap := getMap(result)
	aMap := getMap(resultMap["a"])
	bMap := getMap(aMap["b"])
	if bMap["c"].String != "value1" {
		t.Fatalf("Expected a.b.c to be 'value1'")
	}
	if bMap["d"].String != "value2" {
		t.Fatalf("Expected a.b.d to be 'value2'")
	}
}

func TestMergePatches_RootLevelPatch(t *testing.T) {
	data := ir.FromMap(map[string]*ir.Node{
		"key": ir.FromString("value"),
	})
	patch := makePatcherData("", data)

	result, err := MergePatches([]*PatcherData{patch})
	if err != nil {
		t.Fatalf("MergePatches returned error: %v", err)
	}

	// Root-level patch should return the patch data directly
	if result.Type != ir.ObjectType {
		t.Fatalf("Expected ObjectType, got %v", result.Type)
	}
	resultMap := getMap(result)
	if resultMap["key"].String != "value" {
		t.Fatalf("Expected root-level patch to contain key='value'")
	}
}

func TestMergePatches_ArrayIndices(t *testing.T) {
	patch1 := makePatcherData("arr[0]", ir.FromString("first"))
	patch2 := makePatcherData("arr[1]", ir.FromString("second"))

	result, err := MergePatches([]*PatcherData{patch1, patch2})
	if err != nil {
		t.Fatalf("MergePatches returned error: %v", err)
	}

	// Should create: {arr: <!arraydiff map with indices>}
	resultMap := getMap(result)
	arrNode := resultMap["arr"]
	if arrNode.Type != ir.ObjectType {
		t.Fatalf("Expected ObjectType for array, got %v", arrNode.Type)
	}
	if arrNode.Tag != "!arraydiff" {
		t.Fatalf("Expected !arraydiff tag, got %q", arrNode.Tag)
	}

	arrMap, err := getIntKeysMap(arrNode)
	if err != nil {
		t.Fatalf("Failed to get int keys map: %v", err)
	}
	if arrMap[0].String != "first" {
		t.Fatalf("Expected arr[0] to be 'first'")
	}
	if arrMap[1].String != "second" {
		t.Fatalf("Expected arr[1] to be 'second'")
	}
}

func TestMergePatches_SparseArrayIndices(t *testing.T) {
	patch1 := makePatcherData("arr{0}", ir.FromString("first"))
	patch2 := makePatcherData("arr{42}", ir.FromString("sparse"))

	result, err := MergePatches([]*PatcherData{patch1, patch2})
	if err != nil {
		t.Fatalf("MergePatches returned error: %v", err)
	}

	// Should create: {arr: <int keys map>}
	resultMap := getMap(result)
	arrNode := resultMap["arr"]
	if arrNode.Type != ir.ObjectType {
		t.Fatalf("Expected ObjectType for sparse array, got %v", arrNode.Type)
	}
	// Sparse arrays don't have the !arraydiff tag
	if arrNode.Tag == "!arraydiff" {
		t.Fatalf("Expected no !arraydiff tag for sparse array")
	}

	arrMap, err := getIntKeysMap(arrNode)
	if err != nil {
		t.Fatalf("Failed to get int keys map: %v", err)
	}
	if arrMap[0].String != "first" {
		t.Fatalf("Expected arr{0} to be 'first'")
	}
	if arrMap[42].String != "sparse" {
		t.Fatalf("Expected arr{42} to be 'sparse'")
	}
}

func TestMergePatches_ConflictingPaths_Descendant(t *testing.T) {
	patch1 := makePatcherData("a.b", ir.FromString("value1"))
	patch2 := makePatcherData("a.b.c", ir.FromString("value2"))

	_, err := MergePatches([]*PatcherData{patch1, patch2})
	if err == nil {
		t.Fatal("Expected error for conflicting paths (descendant)")
	}
	if !contains(err.Error(), "conflicts") {
		t.Fatalf("Expected error about conflicts, got: %v", err)
	}
}

func TestMergePatches_ConflictingPaths_Ancestor(t *testing.T) {
	patch1 := makePatcherData("a.b.c", ir.FromString("value1"))
	patch2 := makePatcherData("a.b", ir.FromString("value2"))

	_, err := MergePatches([]*PatcherData{patch1, patch2})
	if err == nil {
		t.Fatal("Expected error for conflicting paths (ancestor)")
	}
	if !contains(err.Error(), "conflicts") {
		t.Fatalf("Expected error about conflicts, got: %v", err)
	}
}

func TestMergePatches_ConflictingPaths_ExactMatch(t *testing.T) {
	patch1 := makePatcherData("a.b", ir.FromString("value1"))
	patch2 := makePatcherData("a.b", ir.FromString("value2"))

	_, err := MergePatches([]*PatcherData{patch1, patch2})
	if err == nil {
		t.Fatal("Expected error for exact path conflict")
	}
	if !contains(err.Error(), "conflicts") {
		t.Fatalf("Expected error about conflicts, got: %v", err)
	}
}

func TestMergePatches_MixedAccessors_ObjectAndArray(t *testing.T) {
	patch1 := makePatcherData("a.b", ir.FromString("value1"))
	patch2 := makePatcherData("a[0]", ir.FromString("value2"))

	_, err := MergePatches([]*PatcherData{patch1, patch2})
	if err == nil {
		t.Fatal("Expected error for mixed accessors (object and array)")
	}
	if !contains(err.Error(), "mixed accessors") {
		t.Fatalf("Expected error about mixed accessors, got: %v", err)
	}
}

func TestMergePatches_MixedAccessors_ArrayAndSparseArray(t *testing.T) {
	patch1 := makePatcherData("a[0]", ir.FromString("value1"))
	patch2 := makePatcherData("a{0}", ir.FromString("value2"))

	_, err := MergePatches([]*PatcherData{patch1, patch2})
	if err == nil {
		t.Fatal("Expected error for mixed accessors (array and sparse array)")
	}
	if !contains(err.Error(), "mixed accessors") {
		t.Fatalf("Expected error about mixed accessors, got: %v", err)
	}
}

func TestMergePatches_InvalidPath(t *testing.T) {
	// Create a patch with invalid path
	patch := &PatcherData{
		ReceivedAt: time.Now(),
		API: &api.Patch{
			Patch: api.Body{
				Path: "invalid[", // Invalid KPath
				Data: ir.FromString("value"),
			},
		},
	}

	_, err := MergePatches([]*PatcherData{patch})
	if err == nil {
		t.Fatal("Expected error for invalid path")
	}
}

func TestMergePatches_ComplexNested(t *testing.T) {
	patch1 := makePatcherData("user.name", ir.FromString("Alice"))
	patch2 := makePatcherData("user.age", ir.FromInt(30))
	patch3 := makePatcherData("user.addresses[0].street", ir.FromString("123 Main St"))
	patch4 := makePatcherData("user.addresses[0].city", ir.FromString("Springfield"))
	patch5 := makePatcherData("metadata.version", ir.FromString("1.0"))

	result, err := MergePatches([]*PatcherData{patch1, patch2, patch3, patch4, patch5})
	if err != nil {
		t.Fatalf("MergePatches returned error: %v", err)
	}

	// Verify user.name
	resultMap := getMap(result)
	userMap := getMap(resultMap["user"])
	if userMap["name"].String != "Alice" {
		t.Fatalf("Expected user.name to be 'Alice'")
	}

	// Verify user.age
	if userMap["age"].Int64 == nil || *userMap["age"].Int64 != 30 {
		t.Fatalf("Expected user.age to be 30")
	}

	// Verify user.addresses[0].street
	addressesNode := userMap["addresses"]
	if addressesNode.Type != ir.ObjectType || addressesNode.Tag != "!arraydiff" {
		t.Fatalf("Expected addresses to be !arraydiff ObjectType")
	}
	addrMap, err := getIntKeysMap(addressesNode)
	if err != nil {
		t.Fatalf("Failed to get addresses map: %v", err)
	}
	addr0Map := getMap(addrMap[0])
	if addr0Map["street"].String != "123 Main St" {
		t.Fatalf("Expected user.addresses[0].street to be '123 Main St'")
	}
	if addr0Map["city"].String != "Springfield" {
		t.Fatalf("Expected user.addresses[0].city to be 'Springfield'")
	}

	// Verify metadata.version
	metadataMap := getMap(resultMap["metadata"])
	if metadataMap["version"].String != "1.0" {
		t.Fatalf("Expected metadata.version to be '1.0'")
	}
}

// Helper function to check if a string contains a substring
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
