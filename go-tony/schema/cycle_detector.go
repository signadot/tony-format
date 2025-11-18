package schema

import (
	"fmt"
	"strings"

	"github.com/go-air/gini"
	"github.com/go-air/gini/logic"
	"github.com/go-air/gini/z"
	"github.com/signadot/tony-format/go-tony/ir"
)

// edge represents a dependency edge in the dependency graph
type edge struct {
	From      string // Source definition name
	To        string // Target definition name
	FieldName string // Field name where the reference occurs
	IsArray   bool   // Whether the reference is wrapped in an array type
	IsNullable bool  // Whether the reference allows null
	IsOptional bool  // Whether the field is optional (not in match pattern)
}

// dependencyGraph represents the dependency graph of schema definitions
type dependencyGraph struct {
	Edges []edge
	Nodes map[string]bool // All definition names
}

// cycle represents a detected cycle in the dependency graph
type cycle struct {
	Path      []string // Path of definition names forming the cycle
	Edges     []edge   // Edges in the cycle
	HasEscape bool     // Whether the cycle has an escape hatch
}

// buildDependencyGraph builds a dependency graph from schema definitions
func buildDependencyGraph(define map[string]*ir.Node) (*dependencyGraph, error) {
	graph := &dependencyGraph{
		Edges: make([]edge, 0),
		Nodes: make(map[string]bool),
	}

	// Collect all definition names
	for name := range define {
		graph.Nodes[name] = true
	}

	// Build edges by traversing each definition
	for defName, defNode := range define {
		if defNode == nil {
			continue
		}
		edges, err := extractReferences(defName, defNode, "", false, false, false)
		if err != nil {
			return nil, fmt.Errorf("failed to extract references from definition %q: %w", defName, err)
		}
		graph.Edges = append(graph.Edges, edges...)
	}

	return graph, nil
}

// extractReferences extracts all .name references from an IR node
func extractReferences(defName string, node *ir.Node, fieldName string, inArray bool, inNullable bool, isOptional bool) ([]edge, error) {
	if node == nil {
		return nil, nil
	}
	
	var edges []edge

	// Check if this node is a reference (tag starts with "." or string value starts with ".")
	var refName string
	var isRefArray bool
	if node.Tag != "" && strings.HasPrefix(node.Tag, ".") {
		refName = extractRefName(node.Tag)
		// Check if the reference itself is wrapped in an array type constructor
		isRefArray = strings.HasPrefix(node.Tag, ".array") || strings.HasPrefix(node.Tag, ".sparsearray")
	} else if node.Type == ir.StringType && strings.HasPrefix(node.String, ".") {
		refName = extractRefName(node.String)
		// Check if the reference itself is wrapped in an array type constructor
		isRefArray = strings.HasPrefix(node.String, ".array") || strings.HasPrefix(node.String, ".sparsearray")
	}
	
	if refName != "" {
		edges = append(edges, edge{
			From:       defName,
			To:         refName,
			FieldName:  fieldName,
			IsArray:    inArray || isRefArray,
			IsNullable: inNullable,
			IsOptional: isOptional,
		})
	}

	// Check if this is an array type (provides escape hatch for child references)
	isArrayType := isArrayTypeNode(node)
	if isArrayType {
		inArray = true
	}

	// Check if this is nullable (or an escape hatch like array)
	// isNullableTypeNode now checks for nullable OR array, so arrays are handled here too
	isNullableType := isNullableTypeNode(node)
	if isNullableType {
		// If it's an array, set inArray flag
		if isArrayType {
			inArray = true
		} else {
			// If it's nullable (but not an array), set inNullable flag
			// Arrays are handled separately via inArray flag
			// Check if it's actually nullable (not just an array)
			if node.Type == ir.NullType {
				inNullable = true
			} else if tag := node.Tag; tag != "" {
				// Use SAT solver to check if it's nullable
				// The SAT solver checks for escape hatches (null OR array)
				// but we've already handled arrays above, so if it returns true
				// and it's not an array, it must be nullable
				if isNullableTypeNode(node) && !isArrayTypeNode(node) {
					inNullable = true
				}
			}
		}
	}

	// Traverse children
	switch node.Type {
	case ir.ObjectType:
		// For objects, traverse fields
		for i := range node.Fields {
			fieldNode := node.Fields[i]
			valueNode := node.Values[i]
			if fieldNode == nil || valueNode == nil {
				continue
			}
			fieldNameStr := ""
			if fieldNode.Type == ir.StringType {
				fieldNameStr = fieldNode.String
			}
			// TODO: Check if field is in match pattern to determine if optional
			// For now, assume fields are required unless we can determine otherwise
			childEdges, err := extractReferences(defName, valueNode, fieldNameStr, inArray, inNullable, isOptional)
			if err != nil {
				return nil, err
			}
			edges = append(edges, childEdges...)
		}
	case ir.ArrayType:
		// For arrays, traverse elements
		for _, elem := range node.Values {
			childEdges, err := extractReferences(defName, elem, fieldName, true, inNullable, isOptional)
			if err != nil {
				return nil, err
			}
			edges = append(edges, childEdges...)
		}
	}

	return edges, nil
}

// extractRefName extracts the definition name from a reference tag like ".node" or ".array(.node)"
func extractRefName(tag string) string {
	if tag == "" || !strings.HasPrefix(tag, ".") {
		return ""
	}

	// Remove the leading "."
	rest := tag[1:]

	// Parse the tag structure manually since ir.TagArgs expects "!" prefix
	// Handle cases like ".node", ".array(.node)", ".array(t)"
	
	// Find the first identifier (before any parentheses)
	firstDot := strings.Index(rest, "(")
	if firstDot == -1 {
		// Simple reference like ".node"
		return strings.TrimSpace(rest)
	}

	// Has parentheses like ".array(.node)" or ".array(t)"
	head := strings.TrimSpace(rest[:firstDot])
	argPart := rest[firstDot+1:]
	
	// Find matching closing parenthesis
	parenDepth := 1
	argEnd := -1
	for i := 0; i < len(argPart); i++ {
		if argPart[i] == '(' {
			parenDepth++
		} else if argPart[i] == ')' {
			parenDepth--
			if parenDepth == 0 {
				argEnd = i
				break
			}
		}
	}
	
	if argEnd < 0 {
		// Malformed, return the head
		return head
	}
	
	arg := strings.TrimSpace(argPart[:argEnd])
	
	// Check if this is a type constructor that wraps a reference
	typeConstructors := map[string]bool{
		"array":       true,
		"sparsearray": true,
	}
	
	if typeConstructors[head] {
		// Check if the argument is a reference
		if strings.HasPrefix(arg, ".") {
			// Recursively extract the reference from the argument
			return extractRefName(arg)
		}
		// Otherwise, this is a type parameter like "t", not a reference
		return ""
	}
	
	// Not a known type constructor, return the head as-is
	return head
}

// isArrayTypeNode checks if a node represents an array type
func isArrayTypeNode(node *ir.Node) bool {
	if node == nil {
		return false
	}
	// Check tag for array-related tags
	tag := node.Tag
	if tag == "" {
		return false
	}
	// Check for array type tags
	head, _, _ := ir.TagArgs(tag)
	return head == ".array" || head == "array" || strings.HasPrefix(head, ".array") || strings.HasPrefix(head, "array")
}

// isEscapeHatchNode checks if a node represents an escape hatch
// An escape hatch is: nullable OR array OR optional
// This is used to determine if a cycle can be broken
func isEscapeHatchNode(node *ir.Node) bool {
	if node == nil {
		return false
	}
	
	// Direct null type is an escape hatch
	if node.Type == ir.NullType {
		return true
	}
	
	// Array types are escape hatches
	if isArrayTypeNode(node) {
		return true
	}
	
	// Nullable types are escape hatches
	if isNullableTypeNode(node) {
		return true
	}
	
	return false
}

// isNullableTypeNode checks if a node represents an escape hatch
// An escape hatch is: nullable OR array
// Uses a SAT solver to handle all boolean combinations of !not, !and, and !or
// Both arrays and null are treated identically as escape hatches
func isNullableTypeNode(node *ir.Node) bool {
	if node == nil {
		return false
	}
	
	// Direct null type is an escape hatch
	if node.Type == ir.NullType {
		return true
	}
	
	// Array types are escape hatches
	if isArrayTypeNode(node) {
		return true
	}
	
	tag := node.Tag
	if tag == "" {
		return false
	}
	
	// Build a boolean formula using gini/logic
	// Variable represents "isEscapeHatch" (can be null OR array)
	c := logic.NewC()
	isEscapeHatch := c.Lit() // Variable representing "value is an escape hatch" (null or array)
	
	// Build the formula representing the type constraint
	formula := buildBooleanFormula(c, node, tag, isEscapeHatch)
	if formula == z.LitNull {
		return false
	}
	
	// Convert to CNF and check satisfiability
	g := gini.New()
	c.ToCnf(g)
	
	// We want to check if the formula can be satisfied when isEscapeHatch is true
	// So we assume: formula AND isEscapeHatch
	g.Assume(formula)
	g.Assume(isEscapeHatch)
	
	// Check satisfiability
	result := g.Solve()
	return result == 1 // 1 means satisfiable
}

// buildBooleanFormula converts an IR node with a tag into a boolean formula using gini/logic
// Returns a literal representing the formula (true if the constraint is satisfied)
// isEscapeHatch is the literal representing "value is an escape hatch" (null or array)
func buildBooleanFormula(c *logic.C, node *ir.Node, tag string, isEscapeHatch z.Lit) z.Lit {
	if tag == "" {
		return z.LitNull
	}
	
	head, _, rest := ir.TagArgs(tag)
	
	// Handle !not
	if head == "!not" {
		// Special case: !not null explicitly excludes escape hatch
		// !not null is represented as tag "!not" with node.Type == Null
		if node.Type == ir.NullType {
			return isEscapeHatch.Not()
		}
		if rest != "" {
			// !not.something - recursively build formula and negate
			// If rest is "or" or "and", we need to handle it as !or or !and
			// by reconstructing the tag with the ! prefix
			innerTag := rest
			if rest == "or" || rest == "and" {
				innerTag = "!" + rest
			}
			innerFormula := buildBooleanFormula(c, node, innerTag, isEscapeHatch)
			if innerFormula == z.LitNull {
				return z.LitNull
			}
			// For negation, we create a new literal and constrain it to be !innerFormula
			// notInner <-> !innerFormula means:
			// (notInner -> !innerFormula) AND (!innerFormula -> notInner)
			// Which is: (!notInner OR !innerFormula) AND (innerFormula OR notInner)
			notInner := c.Lit()
			c.And(c.Or(notInner.Not(), innerFormula.Not()), c.Or(innerFormula, notInner))
			return notInner
		}
		if node.Type == ir.ArrayType {
			// Check if any element is null or array
			for _, val := range node.Values {
				if isNullValue(val) || isArrayTypeNode(val) {
					// !not escape hatch - explicitly excludes escape hatch
					return isEscapeHatch.Not()
				}
			}
		}
		// Check if the value itself is null (string "null" or other representation)
		if isNullValue(node) {
			// Formula: !isEscapeHatch (not an escape hatch)
			return isEscapeHatch.Not()
		}
		// !not X where X is not null/array - check inner tag
		if node.Tag != "" && node.Tag != tag {
			innerFormula := buildBooleanFormula(c, node, node.Tag, isEscapeHatch)
			if innerFormula == z.LitNull {
				return z.LitNull
			}
			// Negate the inner formula
			notInner := c.Lit()
			c.And(c.Or(notInner.Not(), innerFormula.Not()), c.Or(innerFormula, notInner))
			return notInner
		}
		return z.LitNull
	}
	
	// Handle !or
	if head == "!or" {
		if node.Type == ir.ArrayType {
			// OR: at least one element must be satisfied
			literals := []z.Lit{}
			for _, val := range node.Values {
				if isNullValue(val) || isArrayTypeNode(val) {
					// Null or array - both are escape hatches
					literals = append(literals, isEscapeHatch)
				} else if val.Tag != "" {
					// Recursively build formula for this element
					valFormula := buildBooleanFormula(c, val, val.Tag, isEscapeHatch)
					if valFormula != z.LitNull {
						literals = append(literals, valFormula)
					}
				} else {
					// Recursively check if it's an escape hatch
					if isNullableTypeNode(val) {
						literals = append(literals, isEscapeHatch)
					}
				}
			}
			if len(literals) > 0 {
				return c.Ors(literals...)
			}
			return z.LitNull
		}
		// !or/or tags are always represented as arrays in IR
		return z.LitNull
	}
	
	// Handle !and
	if head == "!and" {
		if node.Type == ir.ArrayType {
			// AND: all elements must be satisfied
			literals := []z.Lit{}
			hasExplicitNull := false
			
			for _, val := range node.Values {
				if isNullValue(val) || isArrayTypeNode(val) {
					// Null or array - both are escape hatches
					literals = append(literals, isEscapeHatch)
					hasExplicitNull = true
				} else if val.Tag != "" {
					// Recursively build formula for this element
					valFormula := buildBooleanFormula(c, val, val.Tag, isEscapeHatch)
					if valFormula != z.LitNull {
						literals = append(literals, valFormula)
					} else if !hasExplicitNull {
						// If element doesn't allow escape hatch, AND cannot be satisfied
						return z.LitNull
					}
				} else {
					// Recursively check if it's an escape hatch
					if isNullableTypeNode(val) {
						literals = append(literals, isEscapeHatch)
						hasExplicitNull = true
					} else if !hasExplicitNull {
						// Element doesn't allow escape hatch, and no explicit escape hatch
						return z.LitNull
					}
				}
			}
			
			if len(literals) > 0 {
				return c.Ands(literals...)
			}
			return z.LitNull
		}
		return z.LitNull
	}
	
	// If we have a rest (chained tags like !not.and), recursively check
	if rest != "" {
		return buildBooleanFormula(c, node, rest, isEscapeHatch)
	}
	
	return z.LitNull
}

// isNullValue checks if a node represents a null value
func isNullValue(node *ir.Node) bool {
	if node == nil {
		return false
	}
	if node.Type == ir.NullType {
		return true
	}
	if node.Type == ir.StringType && node.String == "null" {
		return true
	}
	// Check tag for null reference
	if node.Tag != "" {
		head, args, _ := ir.TagArgs(node.Tag)
		if head == "null" || (len(args) > 0 && args[0] == "null") {
			return true
		}
	}
	return false
}

// findCycles finds all cycles in the dependency graph using DFS with colors
// white=unvisited, grey=visiting, black=done
func findCycles(graph *dependencyGraph) [][]string {
	color := make(map[string]int) // 0=white, 1=grey, 2=black
	var cycles [][]string

	var dfs func(string, []string)
	dfs = func(node string, path []string) {
		color[node] = 1 // grey - visiting
		path = append(path, node)

		// Check all outgoing edges
		for _, e := range graph.Edges {
			if e.From != node {
				continue
			}
			to := e.To

			if color[to] == 1 {
				// Found a back edge to a grey node - cycle detected
				// Find the cycle path
				cycleStart := -1
				for i, n := range path {
					if n == to {
						cycleStart = i
						break
					}
				}
				if cycleStart >= 0 {
					cycle := make([]string, len(path)-cycleStart+1)
					copy(cycle, path[cycleStart:])
					cycle[len(cycle)-1] = to
					cycles = append(cycles, cycle)
				}
			} else if color[to] == 0 {
				// White - unvisited, recurse with a copy of the path
				pathCopy := make([]string, len(path))
				copy(pathCopy, path)
				dfs(to, pathCopy)
			}
			// Black nodes are already done, skip
		}

		color[node] = 2 // black - done
	}

	// Process all nodes
	for node := range graph.Nodes {
		if color[node] == 0 {
			dfs(node, nil)
		}
	}

	return cycles
}

// analyzeCycle analyzes a cycle to determine if it has escape hatches
func analyzeCycle(graph *dependencyGraph, cyclePath []string) cycle {
	// Build a map for quick lookup
	cycleMap := make(map[string]bool)
	for _, node := range cyclePath {
		cycleMap[node] = true
	}

	// Find all edges in the cycle
	var cycleEdges []edge
	for _, e := range graph.Edges {
		if cycleMap[e.From] && cycleMap[e.To] {
			cycleEdges = append(cycleEdges, e)
		}
	}

	// Check if any edge has an escape hatch
	hasEscape := false
	for _, e := range cycleEdges {
		if e.IsArray || e.IsNullable || e.IsOptional {
			hasEscape = true
			break
		}
	}

	return cycle{
		Path:      cyclePath,
		Edges:     cycleEdges,
		HasEscape: hasEscape,
	}
}

// ValidateCycles validates that all cycles in schema definitions have escape hatches
func ValidateCycles(schema *Schema) error {
	if schema == nil || schema.Define == nil || len(schema.Define) == 0 {
		return nil
	}

	// Build dependency graph
	graph, err := buildDependencyGraph(schema.Define)
	if err != nil {
		return fmt.Errorf("failed to build dependency graph: %w", err)
	}

	// Find all cycles
	cycles := findCycles(graph)

	// Analyze each cycle
	var impossibleCycles []cycle
	for _, cyclePath := range cycles {
		cyc := analyzeCycle(graph, cyclePath)
		if !cyc.HasEscape {
			impossibleCycles = append(impossibleCycles, cyc)
		}
	}
	
	// Report impossible cycles
	if len(impossibleCycles) > 0 {
		var msgs []string
		for _, cyc := range impossibleCycles {
			msg := fmt.Sprintf("impossible cycle detected: %s", strings.Join(cyc.Path, " -> "))
			if len(cyc.Edges) > 0 {
				var edgeDescs []string
				for _, e := range cyc.Edges {
					edgeDesc := fmt.Sprintf("%s.%s -> %s", e.From, e.FieldName, e.To)
					edgeDescs = append(edgeDescs, edgeDesc)
				}
				msg += fmt.Sprintf(" (edges: %s)", strings.Join(edgeDescs, ", "))
			}
			msg += " - no escape hatches (make fields nullable, use array types, or make fields optional)"
			msgs = append(msgs, msg)
		}
		return fmt.Errorf("schema validation failed:\n%s", strings.Join(msgs, "\n"))
	}

	return nil
}
