package codegen

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"
)

// DependencyGraph represents a graph of struct dependencies.
type DependencyGraph struct {
	Nodes map[string]*StructInfo
	Edges map[string][]string // Adjacency list: structName -> []dependencyNames
}

// BuildDependencyGraph builds a dependency graph from a list of structs.
// A dependency exists if struct A has a field of type struct B.
func BuildDependencyGraph(structs []*StructInfo) (*DependencyGraph, error) {
	graph := &DependencyGraph{
		Nodes: make(map[string]*StructInfo),
		Edges: make(map[string][]string),
	}

	// Populate nodes
	for _, s := range structs {
		graph.Nodes[s.Name] = s
		graph.Edges[s.Name] = []string{}
	}

	// Populate edges
	for _, s := range structs {
		deps := make(map[string]bool) // Use map to avoid duplicates

		for _, field := range s.Fields {
			// Skip omitted fields
			if field.Omit {
				continue
			}

			// Find dependencies from field type
			// We need to check if the field type references another struct in our list
			depName := extractDependency(field, graph.Nodes)
			if depName != "" && depName != s.Name { // Ignore self-references
				deps[depName] = true
			}
		}

		// Add edges to graph
		for dep := range deps {
			graph.Edges[s.Name] = append(graph.Edges[s.Name], dep)
		}

		// Sort edges for deterministic output
		sort.Strings(graph.Edges[s.Name])
	}

	return graph, nil
}

// extractDependency extracts the name of the struct dependency from a field.
// Returns empty string if no dependency found or dependency is not in our struct list.
func extractDependency(field *FieldInfo, nodes map[string]*StructInfo) string {
	// Try to get dependency from StructTypeName (set by parser/reflection)
	if field.StructTypeName != "" {
		if _, ok := nodes[field.StructTypeName]; ok {
			return field.StructTypeName
		}
	}

	// Fallback to AST analysis if StructTypeName is missing (e.g. AST-only mode)
	if field.ASTType != nil {
		return extractDependencyFromAST(field.ASTType, nodes)
	}

	return ""
}

// extractDependencyFromAST extracts dependency from AST type expression.
func extractDependencyFromAST(expr ast.Expr, nodes map[string]*StructInfo) string {
	switch t := expr.(type) {
	case *ast.Ident:
		// Simple type name (e.g. Person)
		if _, ok := nodes[t.Name]; ok {
			return t.Name
		}
	case *ast.StarExpr:
		// Pointer type (e.g. *Person)
		return extractDependencyFromAST(t.X, nodes)
	case *ast.ArrayType:
		// Slice/Array type (e.g. []Person)
		return extractDependencyFromAST(t.Elt, nodes)
	case *ast.MapType:
		// Map type (e.g. map[string]Person)
		// Check value type
		dep := extractDependencyFromAST(t.Value, nodes)
		if dep != "" {
			return dep
		}
		// Check key type (unlikely for struct keys, but possible)
		return extractDependencyFromAST(t.Key, nodes)
	}
	return ""
}

// Cycle represents a circular dependency path.
type Cycle []string

// DetectCycles detects circular dependencies in the graph.
// Returns a list of cycles found, or nil if no cycles.
func DetectCycles(graph *DependencyGraph) ([]Cycle, error) {
	var cycles []Cycle
	visited := make(map[string]bool)
	recursionStack := make(map[string]bool)
	path := []string{}

	// Sort nodes for deterministic order
	var nodes []string
	for name := range graph.Nodes {
		nodes = append(nodes, name)
	}
	sort.Strings(nodes)

	for _, node := range nodes {
		if !visited[node] {
			if err := dfs(node, graph, visited, recursionStack, &path, &cycles); err != nil {
				return nil, err
			}
		}
	}

	if len(cycles) > 0 {
		return cycles, nil
	}
	return nil, nil
}

func dfs(node string, graph *DependencyGraph, visited, recursionStack map[string]bool, path *[]string, cycles *[]Cycle) error {
	visited[node] = true
	recursionStack[node] = true
	*path = append(*path, node)

	for _, neighbor := range graph.Edges[node] {
		if !visited[neighbor] {
			if err := dfs(neighbor, graph, visited, recursionStack, path, cycles); err != nil {
				return err
			}
		} else if recursionStack[neighbor] {
			// Cycle detected!
			// Extract cycle from path
			cycle := make(Cycle, 0)
			startIdx := -1
			for i, n := range *path {
				if n == neighbor {
					startIdx = i
					break
				}
			}
			if startIdx != -1 {
				cycle = append(cycle, (*path)[startIdx:]...)
				// Add the closing node to show the loop
				cycle = append(cycle, neighbor)
				*cycles = append(*cycles, cycle)
			}
		}
	}

	recursionStack[node] = false
	*path = (*path)[:len(*path)-1]
	return nil
}

// TopologicalSort returns the structs in dependency order (dependencies first).
// Returns error if cycles are detected.
func TopologicalSort(graph *DependencyGraph) ([]*StructInfo, error) {
	// First check for cycles
	cycles, err := DetectCycles(graph)
	if err != nil {
		return nil, err
	}
	if len(cycles) > 0 {
		var cycleMsgs []string
		for _, c := range cycles {
			cycleMsgs = append(cycleMsgs, strings.Join(c, " -> "))
		}
		return nil, fmt.Errorf("circular dependencies detected:\n%s", strings.Join(cycleMsgs, "\n"))
	}

	visited := make(map[string]bool)
	var result []*StructInfo

	// Sort nodes for deterministic order
	var nodes []string
	for name := range graph.Nodes {
		nodes = append(nodes, name)
	}
	sort.Strings(nodes)

	// Helper for post-order traversal
	var visit func(string)
	visit = func(node string) {
		if visited[node] {
			return
		}
		visited[node] = true

		for _, neighbor := range graph.Edges[node] {
			visit(neighbor)
		}

		result = append(result, graph.Nodes[node])
	}

	for _, node := range nodes {
		visit(node)
	}

	return result, nil
}
