package codegen

import (
	"fmt"
	"go/ast"
	"strings"
)

// DependencyGraph represents a directed graph of struct dependencies.
// An edge A -> B means struct A depends on struct B (A has a field of type B).
type DependencyGraph struct {
	// Nodes maps struct name to StructInfo
	Nodes map[string]*StructInfo

	// Edges maps struct name to list of struct names it depends on
	Edges map[string][]string

	// ReverseEdges maps struct name to list of struct names that depend on it
	ReverseEdges map[string][]string
}

// Cycle represents a circular dependency detected in the graph.
type Cycle struct {
	// Path is the sequence of struct names forming the cycle
	Path []string
}

// BuildDependencyGraph builds a dependency graph from a list of structs.
// Only structs with schemadef= tags are included in the graph (they're the ones we generate schemas for).
// Dependencies are detected by analyzing field types that reference other structs.
func BuildDependencyGraph(structs []*StructInfo) (*DependencyGraph, error) {
	graph := &DependencyGraph{
		Nodes:        make(map[string]*StructInfo),
		Edges:        make(map[string][]string),
		ReverseEdges: make(map[string][]string),
	}

	// Build a map of struct names for quick lookup
	structMap := make(map[string]*StructInfo)
	for _, s := range structs {
		// Only include structs with schemadef= tags (they generate schemas)
		if s.StructSchema != nil && s.StructSchema.Mode == "schemadef" {
			structMap[s.Name] = s
			graph.Nodes[s.Name] = s
			graph.Edges[s.Name] = []string{}
			graph.ReverseEdges[s.Name] = []string{}
		}
	}

	// Build edges by analyzing field types
	for _, s := range structs {
		// Only process structs with schemadef= tags
		if s.StructSchema == nil || s.StructSchema.Mode != "schemadef" {
			continue
		}

		deps := findDependencies(s, structMap)
		graph.Edges[s.Name] = deps

		// Build reverse edges
		for _, dep := range deps {
			graph.ReverseEdges[dep] = append(graph.ReverseEdges[dep], s.Name)
		}
	}

	return graph, nil
}

// findDependencies finds all struct dependencies for a given struct.
// Returns a list of struct names that this struct depends on.
func findDependencies(structInfo *StructInfo, structMap map[string]*StructInfo) []string {
	var deps []string
	seen := make(map[string]bool)

	for _, field := range structInfo.Fields {
		if field.Omit {
			continue
		}

		// Analyze the AST type to find struct references
		fieldDeps := findStructReferences(field.ASTType, structMap, structInfo.Package)
		for _, dep := range fieldDeps {
			if !seen[dep] {
				seen[dep] = true
				deps = append(deps, dep)
			}
		}
	}

	return deps
}

// findStructReferences recursively analyzes an AST type expression to find struct references.
// Returns a list of struct names that are referenced.
func findStructReferences(expr ast.Expr, structMap map[string]*StructInfo, currentPkg string) []string {
	if expr == nil {
		return nil
	}

	var refs []string

	switch t := expr.(type) {
	case *ast.Ident:
		// Simple identifier - could be a struct type
		if structInfo, ok := structMap[t.Name]; ok {
			// Only include if it's in the same package (cross-package handled separately)
			if structInfo.Package == currentPkg {
				refs = append(refs, t.Name)
			}
		}

	case *ast.StarExpr:
		// Pointer type: *T
		refs = append(refs, findStructReferences(t.X, structMap, currentPkg)...)

	case *ast.ArrayType:
		// Array or slice type: []T or [N]T
		refs = append(refs, findStructReferences(t.Elt, structMap, currentPkg)...)

	case *ast.MapType:
		// Map type: map[K]V
		refs = append(refs, findStructReferences(t.Key, structMap, currentPkg)...)
		refs = append(refs, findStructReferences(t.Value, structMap, currentPkg)...)

	case *ast.SelectorExpr:
		// Qualified identifier: pkg.Type (cross-package reference)
		// For now, we only track same-package dependencies
		// Cross-package dependencies will be handled via schema loading
		// But we can still extract the type name for reference
		if ident, ok := t.X.(*ast.Ident); ok {
			// This is a cross-package reference: pkg.Type
			// We don't add it to dependencies since we'll load the schema separately
			_ = ident // pkg name
			_ = t.Sel // type name
		}

	case *ast.ChanType:
		// Channel type: chan T
		refs = append(refs, findStructReferences(t.Value, structMap, currentPkg)...)

	case *ast.FuncType:
		// Function type - check parameters and return types
		if t.Params != nil {
			for _, param := range t.Params.List {
				refs = append(refs, findStructReferences(param.Type, structMap, currentPkg)...)
			}
		}
		if t.Results != nil {
			for _, result := range t.Results.List {
				refs = append(refs, findStructReferences(result.Type, structMap, currentPkg)...)
			}
		}

	case *ast.InterfaceType:
		// Interface type - no struct dependencies

	case *ast.StructType:
		// Nested struct type - analyze its fields
		if t.Fields != nil {
			for _, field := range t.Fields.List {
				for _, name := range field.Names {
					if !name.IsExported() {
						continue
					}
					refs = append(refs, findStructReferences(field.Type, structMap, currentPkg)...)
				}
			}
		}

	default:
		// Other types (Ellipsis, etc.) - skip for now
	}

	return refs
}

// DetectCycles detects circular dependencies in the dependency graph using DFS.
// Returns a list of cycles found, or nil if no cycles exist.
func DetectCycles(graph *DependencyGraph) ([]*Cycle, error) {
	var cycles []*Cycle
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	path := []string{}

	// DFS from each unvisited node
	for name := range graph.Nodes {
		if !visited[name] {
			cycles = append(cycles, detectCyclesDFS(graph, name, visited, recStack, path)...)
		}
	}

	if len(cycles) > 0 {
		return cycles, fmt.Errorf("circular dependencies detected: %d cycle(s) found", len(cycles))
	}

	return nil, nil
}

// detectCyclesDFS performs DFS to detect cycles starting from a given node.
func detectCyclesDFS(graph *DependencyGraph, node string, visited map[string]bool, recStack map[string]bool, path []string) []*Cycle {
	var cycles []*Cycle

	visited[node] = true
	recStack[node] = true
	path = append(path, node)

	// Check all dependencies
	for _, dep := range graph.Edges[node] {
		if !visited[dep] {
			// Recurse
			cycles = append(cycles, detectCyclesDFS(graph, dep, visited, recStack, path)...)
		} else if recStack[dep] {
			// Found a back edge - cycle detected
			// Find the cycle path
			cyclePath := findCyclePath(path, dep)
			cycles = append(cycles, &Cycle{Path: cyclePath})
		}
	}

	recStack[node] = false
	return cycles
}

// findCyclePath extracts the cycle path from the current DFS path.
func findCyclePath(path []string, cycleStart string) []string {
	// Find where the cycle starts
	startIdx := -1
	for i, name := range path {
		if name == cycleStart {
			startIdx = i
			break
		}
	}

	if startIdx == -1 {
		// Shouldn't happen, but return the full path
		return append(path, cycleStart)
	}

	// Extract the cycle: from cycleStart to the end, then back to cycleStart
	cycle := make([]string, 0, len(path)-startIdx+1)
	cycle = append(cycle, path[startIdx:]...)
	cycle = append(cycle, cycleStart) // Close the cycle
	return cycle
}

// TopologicalSort performs a topological sort on the dependency graph.
// Returns structs in dependency order (dependencies come before dependents).
// Returns an error if cycles are detected.
func TopologicalSort(graph *DependencyGraph) ([]*StructInfo, error) {
	// First check for cycles
	cycles, err := DetectCycles(graph)
	if err != nil {
		// Format cycle error messages
		var cycleMsgs []string
		for _, cycle := range cycles {
			cycleMsgs = append(cycleMsgs, fmt.Sprintf("cycle: %s", strings.Join(cycle.Path, " -> ")))
		}
		return nil, fmt.Errorf("%v: %s", err, strings.Join(cycleMsgs, "; "))
	}

	// Perform topological sort using Kahn's algorithm
	// We want dependencies before dependents, so we process nodes with no dependencies first
	// In-degree = number of dependencies (incoming edges from dependencies)
	inDegree := make(map[string]int)
	for name := range graph.Nodes {
		inDegree[name] = 0
	}

	// Calculate in-degrees
	// If A depends on B (Edges[A] contains B), then A has an incoming dependency from B
	// So A's in-degree should be incremented for each dependency it has
	for name, deps := range graph.Edges {
		inDegree[name] = len(deps)
	}

	// Find nodes with no dependencies (in-degree 0)
	queue := []string{}
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	var result []*StructInfo

	// Process nodes
	for len(queue) > 0 {
		// Dequeue a node
		node := queue[0]
		queue = queue[1:]

		// Add to result
		if structInfo, ok := graph.Nodes[node]; ok {
			result = append(result, structInfo)
		}

		// Reduce in-degree of nodes that depend on this node
		// If we processed B, then nodes that depend on B (have B in their Edges list) can be processed
		for dependent, deps := range graph.Edges {
			for _, dep := range deps {
				if dep == node {
					inDegree[dependent]--
					if inDegree[dependent] == 0 {
						queue = append(queue, dependent)
					}
				}
			}
		}
	}

	// Check if all nodes were processed (should be, since we checked for cycles)
	if len(result) != len(graph.Nodes) {
		return nil, fmt.Errorf("topological sort incomplete: processed %d of %d nodes (this should not happen if no cycles)", len(result), len(graph.Nodes))
	}

	return result, nil
}

// FormatCycleError formats a cycle error message for user display.
func FormatCycleError(cycles []*Cycle) string {
	var msgs []string
	for _, cycle := range cycles {
		msgs = append(msgs, fmt.Sprintf("  %s", strings.Join(cycle.Path, " -> ")))
	}
	return fmt.Sprintf("Circular dependencies detected:\n%s", strings.Join(msgs, "\n"))
}
