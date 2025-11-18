package codegen

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestBuildDependencyGraph_Simple(t *testing.T) {
	src := `package test

type Person struct {
	_    ` + "`tony:\"schemadef=person\"`" + `
	Name string
}

type Employee struct {
	_      ` + "`tony:\"schemadef=employee\"`" + `
	Person Person
}
`

	structs := parseStructsFromSource(t, src)
	graph, err := BuildDependencyGraph(structs)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	// Check nodes
	if len(graph.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(graph.Nodes))
	}

	// Check edges: Employee depends on Person
	if len(graph.Edges["Employee"]) != 1 {
		t.Errorf("expected Employee to have 1 dependency, got %d", len(graph.Edges["Employee"]))
	}
	if graph.Edges["Employee"][0] != "Person" {
		t.Errorf("expected Employee to depend on Person, got %q", graph.Edges["Employee"][0])
	}

	// Check reverse edges: Person has Employee as dependent
	if len(graph.ReverseEdges["Person"]) != 1 {
		t.Errorf("expected Person to have 1 dependent, got %d", len(graph.ReverseEdges["Person"]))
	}
	if graph.ReverseEdges["Person"][0] != "Employee" {
		t.Errorf("expected Person to have Employee as dependent, got %q", graph.ReverseEdges["Person"][0])
	}
}

func TestBuildDependencyGraph_NoDependencies(t *testing.T) {
	src := `package test

type Person struct {
	_    ` + "`tony:\"schemadef=person\"`" + `
	Name string
}

type User struct {
	_    ` + "`tony:\"schemadef=user\"`" + `
	ID   string
}
`

	structs := parseStructsFromSource(t, src)
	graph, err := BuildDependencyGraph(structs)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	// Check nodes
	if len(graph.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(graph.Nodes))
	}

	// Check no edges
	for name, deps := range graph.Edges {
		if len(deps) != 0 {
			t.Errorf("expected %q to have no dependencies, got %d", name, len(deps))
		}
	}
}

func TestBuildDependencyGraph_PointerAndSlice(t *testing.T) {
	src := `package test

type Person struct {
	_    ` + "`tony:\"schemadef=person\"`" + `
	Name string
}

type Employee struct {
	_      ` + "`tony:\"schemadef=employee\"`" + `
	Boss   *Person
	Reports []Person
}
`

	structs := parseStructsFromSource(t, src)
	graph, err := BuildDependencyGraph(structs)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	// Employee depends on Person (via pointer and slice)
	if len(graph.Edges["Employee"]) != 1 {
		t.Errorf("expected Employee to have 1 dependency, got %d", len(graph.Edges["Employee"]))
	}
	if graph.Edges["Employee"][0] != "Person" {
		t.Errorf("expected Employee to depend on Person, got %q", graph.Edges["Employee"][0])
	}
}

func TestDetectCycles_NoCycle(t *testing.T) {
	src := `package test

type Person struct {
	_    ` + "`tony:\"schemadef=person\"`" + `
	Name string
}

type Employee struct {
	_      ` + "`tony:\"schemadef=employee\"`" + `
	Person Person
}
`

	structs := parseStructsFromSource(t, src)
	graph, err := BuildDependencyGraph(structs)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	cycles, err := DetectCycles(graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cycles != nil {
		t.Errorf("expected no cycles, got %d", len(cycles))
	}
}

func TestDetectCycles_CircularDependency(t *testing.T) {
	src := `package test

type Person struct {
	_    ` + "`tony:\"schemadef=person\"`" + `
	Name string
	Boss *Employee
}

type Employee struct {
	_      ` + "`tony:\"schemadef=employee\"`" + `
	Name   string
	Reports []Person
}
`

	structs := parseStructsFromSource(t, src)
	graph, err := BuildDependencyGraph(structs)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	cycles, err := DetectCycles(graph)
	if err == nil {
		t.Fatal("expected error for circular dependency")
	}
	if cycles == nil {
		t.Fatal("expected cycles to be non-nil")
	}
	if len(cycles) == 0 {
		t.Fatal("expected at least one cycle")
	}

	// Check that the cycle contains both Person and Employee
	foundPerson := false
	foundEmployee := false
	for _, cycle := range cycles {
		for _, name := range cycle.Path {
			if name == "Person" {
				foundPerson = true
			}
			if name == "Employee" {
				foundEmployee = true
			}
		}
	}
	if !foundPerson || !foundEmployee {
		t.Errorf("expected cycle to contain Person and Employee, got cycles: %+v", cycles)
	}
}

func TestTopologicalSort_Simple(t *testing.T) {
	src := `package test

type Person struct {
	_    ` + "`tony:\"schemadef=person\"`" + `
	Name string
}

type Employee struct {
	_      ` + "`tony:\"schemadef=employee\"`" + `
	Person Person
}
`

	structs := parseStructsFromSource(t, src)
	graph, err := BuildDependencyGraph(structs)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	sorted, err := TopologicalSort(graph)
	if err != nil {
		t.Fatalf("failed to sort: %v", err)
	}

	if len(sorted) != 2 {
		t.Fatalf("expected 2 structs, got %d", len(sorted))
	}

	// Person should come before Employee (dependency first)
	personIdx := -1
	employeeIdx := -1
	for i, s := range sorted {
		if s.Name == "Person" {
			personIdx = i
		}
		if s.Name == "Employee" {
			employeeIdx = i
		}
	}

	if personIdx == -1 || employeeIdx == -1 {
		t.Fatal("could not find Person or Employee in sorted list")
	}

	if personIdx >= employeeIdx {
		t.Errorf("expected Person (%d) to come before Employee (%d)", personIdx, employeeIdx)
	}
}

func TestTopologicalSort_Complex(t *testing.T) {
	src := `package test

type A struct {
	_ ` + "`tony:\"schemadef=a\"`" + `
}

type B struct {
	_ ` + "`tony:\"schemadef=b\"`" + `
	A A
}

type C struct {
	_ ` + "`tony:\"schemadef=c\"`" + `
	B B
}

type D struct {
	_ ` + "`tony:\"schemadef=d\"`" + `
}
`

	structs := parseStructsFromSource(t, src)
	graph, err := BuildDependencyGraph(structs)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	sorted, err := TopologicalSort(graph)
	if err != nil {
		t.Fatalf("failed to sort: %v", err)
	}

	if len(sorted) != 4 {
		t.Fatalf("expected 4 structs, got %d", len(sorted))
	}

	// Find indices
	indices := make(map[string]int)
	for i, s := range sorted {
		indices[s.Name] = i
	}

	// A should come before B, B before C
	if indices["A"] >= indices["B"] {
		t.Errorf("expected A (%d) before B (%d)", indices["A"], indices["B"])
	}
	if indices["B"] >= indices["C"] {
		t.Errorf("expected B (%d) before C (%d)", indices["B"], indices["C"])
	}
	// D has no dependencies, so it can be anywhere
}

func TestTopologicalSort_CircularDependency(t *testing.T) {
	src := `package test

type Person struct {
	_    ` + "`tony:\"schemadef=person\"`" + `
	Boss *Employee
}

type Employee struct {
	_      ` + "`tony:\"schemadef=employee\"`" + `
	Reports []Person
}
`

	structs := parseStructsFromSource(t, src)
	graph, err := BuildDependencyGraph(structs)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	_, err = TopologicalSort(graph)
	if err == nil {
		t.Fatal("expected error for circular dependency")
	}

	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("expected error message to contain 'circular', got: %v", err)
	}
}

func TestTopologicalSort_ForwardReference(t *testing.T) {
	// Forward reference: Employee references Person, but Person is defined later
	src := `package test

type Employee struct {
	_      ` + "`tony:\"schemadef=employee\"`" + `
	Boss   *Person
}

type Person struct {
	_    ` + "`tony:\"schemadef=person\"`" + `
	Name string
}
`

	structs := parseStructsFromSource(t, src)
	graph, err := BuildDependencyGraph(structs)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	sorted, err := TopologicalSort(graph)
	if err != nil {
		t.Fatalf("failed to sort: %v", err)
	}

	// Person should come before Employee (dependency first)
	personIdx := -1
	employeeIdx := -1
	for i, s := range sorted {
		if s.Name == "Person" {
			personIdx = i
		}
		if s.Name == "Employee" {
			employeeIdx = i
		}
	}

	if personIdx >= employeeIdx {
		t.Errorf("expected Person (%d) to come before Employee (%d) even with forward reference", personIdx, employeeIdx)
	}
}

// Helper function to parse structs from source code
func parseStructsFromSource(t *testing.T, src string) []*StructInfo {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	structs, err := ExtractStructs(file, "test.go")
	if err != nil {
		t.Fatalf("failed to extract structs: %v", err)
	}

	return structs
}
