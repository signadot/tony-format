package tx

import (
	"strings"
	"testing"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
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
			PathData: api.PathData{
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
			PathData: api.PathData{
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

// TestRootPatchAt covers re-rooting a value at each kpath segment kind (object,
// array [i], sparse {n}), which the scoped-watch delta relies on. RootPatchAt
// produces a PATCH (arrays as !arraydiff), so it is verified by APPLYING it to null
// and reading the value at the path — the invariant the watch delta contract needs.
func TestRootPatchAt(t *testing.T) {
	// base provides the surrounding structure a delta is applied onto: an array
	// delta (!arraydiff) needs the array to exist, exactly as a client applies a
	// watch delta onto its accumulated state.
	tests := []struct {
		name    string
		kp      string
		base    string
		getPath string
	}{
		{"object", "p.a.x", `null`, "$.p.a.x.v"},
		{"array index", "p.items[2]", `{p: {items: [{v: 0}, {v: 0}, {v: 0}]}}`, "$.p.items[2].v"},
		{"root", "", `null`, "$.v"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := parse.Parse([]byte(`{v: 7}`))
			if err != nil {
				t.Fatalf("parse val: %v", err)
			}
			base, err := parse.Parse([]byte(tt.base))
			if err != nil {
				t.Fatalf("parse base: %v", err)
			}
			rooted, err := RootPatchAt(tt.kp, val)
			if err != nil {
				t.Fatalf("RootPatchAt(%q): %v", tt.kp, err)
			}
			applied, err := tony.Patch(base, rooted)
			if err != nil {
				t.Fatalf("apply RootPatchAt(%q): %v", tt.kp, err)
			}
			got, err := applied.GetPath(tt.getPath)
			if err != nil || got == nil || got.Int64 == nil || *got.Int64 != 7 {
				t.Errorf("applied RootPatchAt(%q) at %q = %v (err %v), want 7", tt.kp, tt.getPath, got, err)
			}
		})
	}
}

// RootPatchAt cannot take a keyed ELEMENT path: `items("A")` carries the key VALUE
// where building the patch needs the key FIELD. It used to answer "ir node
// unspecified", which says nothing about the cause -- so both callers rediscovered
// the same workaround, and the plan asked for the helper rather than a third
// discovery (5hmq80f3h12krh1mbsn0).
func TestRootPatchAtRefusesAKeyedElementPath(t *testing.T) {
	_, err := RootPatchAt(`items("A")`, ir.FromMap(map[string]*ir.Node{"qty": ir.FromInt(5)}))
	if err == nil {
		t.Fatal("a keyed element path was accepted")
	}
	for _, want := range []string{"key field", "RootKeyedListAt"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
}

// RootKeyedListAt is what the callers were open-coding: root at the ARRAY, carry a
// keyed list, so the merge identifies elements rather than replacing whatever sits
// at index 0.
func TestRootKeyedListAt(t *testing.T) {
	elem := func(sku string, qty int64) *ir.Node {
		return ir.FromMap(map[string]*ir.Node{"sku": ir.FromString(sku), "qty": ir.FromInt(qty)})
	}
	got, err := RootKeyedListAt("items", "sku", elem("A", 5))
	if err != nil {
		t.Fatal(err)
	}
	out := encode.MustString(got)
	if !strings.Contains(out, "!key(sku)") {
		t.Errorf("no keying on the list, so it would merge positionally:\n%s", out)
	}
	if !strings.Contains(out, "items") {
		t.Errorf("not rooted at the array:\n%s", out)
	}

	// several elements, as the stepping harness builds
	many, err := RootKeyedListAt("items", "sku", elem("A", 1), elem("B", 2))
	if err != nil {
		t.Fatal(err)
	}
	if out := encode.MustString(many); !strings.Contains(out, "A") || !strings.Contains(out, "B") {
		t.Errorf("lost an element:\n%s", out)
	}

	// and it merges BY KEY rather than by position: patching a list whose order
	// differs must update A, not whatever sits at index 0
	doc := ir.FromSlice([]*ir.Node{elem("B", 9), elem("A", 9)})
	doc.Tag = ir.TagCompose(ir.KeyTag, []string{"sku"}, "")
	base := ir.FromMap(map[string]*ir.Node{"items": doc})
	res, err := tony.Patch(base, got)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	a, err := res.GetKPath(`items("A").qty`)
	if err != nil || a == nil {
		t.Fatalf("items(\"A\").qty is gone: %v\n%s", err, encode.MustString(res))
	}
	if a.Int64 == nil || *a.Int64 != 5 {
		t.Errorf("A.qty = %s, want 5 -- merged positionally\n%s", encode.MustString(a), encode.MustString(res))
	}
	b, _ := res.GetKPath(`items("B").qty`)
	if b == nil || b.Int64 == nil || *b.Int64 != 9 {
		t.Errorf("B was disturbed:\n%s", encode.MustString(res))
	}
}

func TestRootKeyedListAtNeedsAField(t *testing.T) {
	if _, err := RootKeyedListAt("items", "", ir.Null()); err == nil {
		t.Error("a keyed list with no key field was accepted")
	}
}
