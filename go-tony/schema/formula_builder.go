package schema

// SAT-based Formula Builder for Schema Satisfiability
//
// TODO: We need a way to identify when tags correspond to boolean operations
// over arrays. Currently we hardcode !or and !and, but if someone adds a tag
// like !parity (matches if even number of elements match), we'd need to update
// this code. Consider a registry or tag metadata system.

import (
	"fmt"
	"strings"

	"github.com/go-air/gini"
	"github.com/go-air/gini/logic"
	"github.com/go-air/gini/z"
	"github.com/signadot/tony-format/go-tony/ir"
)

// varDef uniquely identifies a variable: (position, type) pair
type varDef struct {
	position string
	typeName string
}

// formulaBuilder builds a SAT formula from schema IR
type formulaBuilder struct {
	c           *logic.C
	path        string              // current kinded path position
	vars        map[varDef]z.Lit    // (position, type) → literal
	mutexes     map[string][]z.Lit  // position → types seen (for mutex)
	checkingDef string              // definition being checked (self-ref → false)
	defParams   map[string]bool     // parameter names of current definition (e.g., "t" for list(t))
	visiting    map[string]bool     // definitions currently being visited (cycle detection)
	definitions map[string]*ir.Node
	defIndex    map[string]string   // base name → full definition name (e.g., "list" → "list(t)")
	err         error               // first error encountered
}

// newFormulaBuilder creates a new formula builder for checking a definition
func newFormulaBuilder(checkingDef string, definitions map[string]*ir.Node) *formulaBuilder {
	// Build index from base name to full definition name
	defIndex := make(map[string]string)
	for defName := range definitions {
		baseName, _ := ParseDefSignature(defName)
		defIndex[baseName] = defName
	}

	// Extract parameter names from the definition being checked
	defParams := make(map[string]bool)
	if checkingDef != "" {
		_, params := ParseDefSignature(checkingDef)
		for _, p := range params {
			defParams[p] = true
		}
	}

	return &formulaBuilder{
		c:           logic.NewC(),
		path:        "",
		vars:        make(map[varDef]z.Lit),
		mutexes:     make(map[string][]z.Lit),
		checkingDef: checkingDef,
		defParams:   defParams,
		visiting:    make(map[string]bool),
		definitions: definitions,
		defIndex:    defIndex,
	}
}

// build recursively builds a formula from an IR node
func (b *formulaBuilder) build(node *ir.Node) z.Lit {
	if b.err != nil {
		return b.c.F
	}
	if node == nil {
		return b.c.T // nil is trivially satisfiable
	}

	tag := node.Tag

	// Handle tagged nodes first
	if tag != "" {
		return b.buildTagged(node, tag)
	}

	// Handle by node type
	switch node.Type {
	case ir.ObjectType:
		return b.buildObject(node)
	case ir.ArrayType:
		// Untagged array: implicit AND with positional elements
		return b.buildPositionalArray(node)
	case ir.NullType:
		return b.getVar("null")
	case ir.StringType:
		// Check if it's a definition reference like ".[name]" or ".[name(args)]"
		if strings.HasPrefix(node.String, ".[") && strings.HasSuffix(node.String, "]") {
			// Extract the reference: .[list(int)] -> list(int)
			refContent := node.String[2 : len(node.String)-1]
			return b.buildRef(refContent)
		}
		return b.getVar("string")
	case ir.NumberType:
		return b.getVar("number")
	case ir.BoolType:
		return b.getVar("bool")
	default:
		b.err = fmt.Errorf("unsupported node type: %v", node.Type)
		return b.c.F
	}
}

// buildTagged handles nodes with tags
func (b *formulaBuilder) buildTagged(node *ir.Node, tag string) z.Lit {
	head, _, rest := ir.TagArgs(tag)

	switch head {
	case "!not":
		child := node.Clone()
		child.Tag = rest
		return b.build(child).Not()

	case "!or":
		if node.Type == ir.ArrayType {
			return b.buildBooleanArray(node.Values, false)
		}
		b.err = fmt.Errorf("!or requires array, got %v", node.Type)
		return b.c.F

	case "!and":
		if node.Type == ir.ArrayType {
			return b.buildBooleanArray(node.Values, true)
		}
		b.err = fmt.Errorf("!and requires array, got %v", node.Type)
		return b.c.F

	case "!null":
		return b.getVar("null")

	case "!string":
		return b.getVar("string")

	case "!int", "!number", "!float":
		return b.getVar("number")

	case "!bool":
		return b.getVar("bool")

	case "!array":
		if node.Type == ir.ArrayType {
			return b.buildPositionalArray(node)
		}
		return b.getVar("array")

	case "!object":
		if node.Type == ir.ObjectType {
			return b.buildObject(node)
		}
		return b.getVar("object")

	case "!bracket":
		// Formatting tag - doesn't affect satisfiability, just process the content
		child := node.Clone()
		child.Tag = rest
		return b.build(child)

	case "!irtype":
		// !irtype constrains value to the IR type of the exemplar node
		switch node.Type {
		case ir.NullType:
			return b.getVar("null")
		case ir.BoolType:
			return b.getVar("bool")
		case ir.NumberType:
			return b.getVar("number")
		case ir.StringType:
			return b.getVar("string")
		case ir.ArrayType:
			return b.getVar("array")
		case ir.ObjectType:
			return b.getVar("object")
		default:
			b.err = fmt.Errorf("!irtype with unknown node type: %v", node.Type)
			return b.c.F
		}

	case "!all":
		// !all.X constrains all elements to match X
		// For collections (array/object): satisfiable (empty collection works)
		// For scalars: the scalar itself must match the constraint
		if node.Type == ir.ArrayType || node.Type == ir.ObjectType {
			return b.c.T // empty collection satisfies !all
		}
		// Scalar: apply the rest of the tag as a constraint
		if rest != "" {
			child := node.Clone()
			child.Tag = rest
			return b.build(child)
		}
		return b.c.T

	default:
		// Strip the ! prefix for lookups
		tagName := head
		if strings.HasPrefix(tagName, "!") {
			tagName = tagName[1:]
		}

		// Check if it's a type parameter of the definition being checked
		// (e.g., !t when checking list(t))
		if b.defParams[tagName] {
			// Parameter placeholder - represents any type, treat as unconstrained
			return b.c.T
		}

		// Check if it's a reference to a known definition
		// (e.g., !node after instantiating wrapper(node) from wrapper(t))
		if _, ok := b.defIndex[tagName]; ok {
			return b.buildRef(tagName)
		}

		// Unknown tag - set error and return false
		b.err = fmt.Errorf("unknown tag in schema: %s", head)
		return b.c.F
	}
}

// baseTypes maps base type names to their SAT type variables
var baseTypes = map[string]string{
	"bool": "bool", "null": "null", "number": "number",
	"int": "number", "float": "number", "string": "string",
	"array": "array", "object": "object", "sparsearray": "object",
}

// buildRef handles definition references
// refContent is the content inside .[...], e.g., "list(int)" or "node"
func (b *formulaBuilder) buildRef(refContent string) z.Lit {
	// Parse the reference to get base name and args
	baseName, refArgs := ParseDefSignature(refContent)

	// Handle built-in base types directly (no need to look up definitions)
	if len(refArgs) == 0 {
		if typeName, ok := baseTypes[baseName]; ok {
			return b.getVar(typeName)
		}
	}
	// Parameterized base types like array(t), nullable(t) are satisfiable
	if len(refArgs) > 0 {
		switch baseName {
		case "array", "sparsearray", "object", "key":
			return b.c.T // collections are satisfiable (empty works)
		case "nullable":
			return b.c.T // nullable is satisfiable (null works)
		case "field":
			return b.c.T // field union is satisfiable
		}
	}

	// Check for self-reference (explicit check for definition being validated)
	// Only a direct self-reference if refContent matches checkingDef exactly,
	// OR if base names match AND both have the same parameterization pattern.
	// e.g., .[array] when checking array(t) is NOT a self-reference because
	// "array" (non-parameterized) and "array(t)" are different definitions.
	checkingBase, checkingArgs := ParseDefSignature(b.checkingDef)
	if baseName == checkingBase {
		// Same base name - only self-reference if parameterization matches
		if len(refArgs) == 0 && len(checkingArgs) == 0 {
			// Both non-parameterized: array vs array → self-ref
			return b.c.F
		}
		if len(refArgs) > 0 && len(checkingArgs) > 0 {
			// Both parameterized: array(x) vs array(t) → self-ref
			// (the args might be different but it's still the same template)
			return b.c.F
		}
		// One is parameterized, one is not: different definitions
	}

	// Check for cycle via visiting set
	if b.visiting[baseName] {
		// Already visiting this base definition
		// If any arg is not a known definition (i.e., it's a parameter placeholder),
		// treat as self-reference: .[list(t)] inside instantiated list body
		for _, arg := range refArgs {
			if _, ok := b.defIndex[arg]; !ok {
				return b.c.F // Arg is a parameter → self-reference
			}
		}
		// All args are known definitions - check if exact instantiation is being visited
		if b.visiting[refContent] {
			return b.c.F
		}
		// Different instantiation of same base - continue (will add to visiting)
	}

	// Find the definition
	// First try exact match (for non-parameterized defs)
	if def, ok := b.definitions[refContent]; ok && def != nil {
		b.visiting[baseName] = true
		b.visiting[refContent] = true
		result := b.build(def)
		delete(b.visiting, baseName)
		delete(b.visiting, refContent)
		return result
	}

	// Look up by base name in the index
	fullDefName, ok := b.defIndex[baseName]
	if !ok {
		b.err = fmt.Errorf("unknown definition reference: .[%s]", refContent)
		return b.c.F
	}

	def, ok := b.definitions[fullDefName]
	if !ok || def == nil {
		b.err = fmt.Errorf("definition body not found: %s", fullDefName)
		return b.c.F
	}

	// Mark as visiting before building
	b.visiting[baseName] = true
	b.visiting[refContent] = true
	defer func() {
		delete(b.visiting, baseName)
		delete(b.visiting, refContent)
	}()

	// If the definition is parameterized, instantiate it
	_, defParams := ParseDefSignature(fullDefName)
	if len(defParams) > 0 {
		if len(refArgs) != len(defParams) {
			b.err = fmt.Errorf("parameter count mismatch for %s: expected %d, got %d",
				baseName, len(defParams), len(refArgs))
			return b.c.F
		}

		// Convert args to IR nodes
		argNodes := make([]*ir.Node, len(refArgs))
		for i, arg := range refArgs {
			argNodes[i] = ir.FromString(arg)
		}

		// Instantiate the definition body with the args
		instantiated, err := InstantiateDef(def, defParams, argNodes)
		if err != nil {
			b.err = fmt.Errorf("failed to instantiate %s: %w", refContent, err)
			return b.c.F
		}

		return b.build(instantiated)
	}

	// Non-parameterized definition - use directly
	return b.build(def)
}

// buildObject handles object nodes: implicit AND of field constraints
func (b *formulaBuilder) buildObject(node *ir.Node) z.Lit {
	if len(node.Fields) == 0 {
		return b.getVar("object")
	}

	lits := make([]z.Lit, 0, len(node.Fields))
	savedPath := b.path

	for i, field := range node.Fields {
		if field == nil || i >= len(node.Values) {
			continue
		}
		value := node.Values[i]
		if value == nil {
			continue
		}

		// Get field name
		fieldName := ""
		if field.Type == ir.StringType {
			fieldName = field.String
		}

		// Update path for this field
		if fieldName != "" {
			if savedPath == "" {
				b.path = fieldName
			} else {
				b.path = savedPath + "." + fieldName
			}
		}

		lits = append(lits, b.build(value))
	}

	b.path = savedPath

	if len(lits) == 0 {
		return b.getVar("object")
	}
	return b.c.Ands(lits...)
}

// buildPositionalArray handles untagged arrays: implicit AND with positional elements
func (b *formulaBuilder) buildPositionalArray(node *ir.Node) z.Lit {
	if len(node.Values) == 0 {
		return b.getVar("array")
	}

	lits := make([]z.Lit, 0, len(node.Values))
	savedPath := b.path

	for i, elem := range node.Values {
		if elem == nil {
			continue
		}

		// Update path with array index
		if savedPath == "" {
			b.path = fmt.Sprintf("[%d]", i)
		} else {
			b.path = fmt.Sprintf("%s[%d]", savedPath, i)
		}

		lits = append(lits, b.build(elem))
	}

	b.path = savedPath

	if len(lits) == 0 {
		return b.getVar("array")
	}
	return b.c.Ands(lits...) // implicit AND for piecewise matching
}

// buildBooleanArray handles !or and !and arrays: elements stay at same position
func (b *formulaBuilder) buildBooleanArray(elements []*ir.Node, isAnd bool) z.Lit {
	if len(elements) == 0 {
		if isAnd {
			return b.c.T // empty AND is true
		}
		return b.c.F // empty OR is false
	}

	lits := make([]z.Lit, 0, len(elements))
	for _, elem := range elements {
		if elem == nil {
			continue
		}
		lits = append(lits, b.build(elem))
	}

	if isAnd {
		return b.c.Ands(lits...)
	}
	return b.c.Ors(lits...)
}

// getVar gets or creates a variable for (position, type), adding mutex constraints
func (b *formulaBuilder) getVar(typeName string) z.Lit {
	key := varDef{b.path, typeName}
	if lit, ok := b.vars[key]; ok {
		return lit // same (position, type) → same variable
	}

	lit := b.c.Lit()
	b.vars[key] = lit
	b.mutexes[b.path] = append(b.mutexes[b.path], lit)

	return lit
}

// addMutexClauses adds mutex clauses to the solver for all positions
func (b *formulaBuilder) addMutexClauses(g *gini.Gini) {
	for _, lits := range b.mutexes {
		// For each pair of different types at same position: ¬(t1 ∧ t2)
		for i := 0; i < len(lits); i++ {
			for j := i + 1; j < len(lits); j++ {
				// ¬(lits[i] ∧ lits[j]) = ¬lits[i] ∨ ¬lits[j]
				g.Add(lits[i].Not())
				g.Add(lits[j].Not())
				g.Add(0) // clause terminator
			}
		}
	}
}

// checkSatisfiability checks if the built formula is satisfiable
func (b *formulaBuilder) checkSatisfiability(formula z.Lit) bool {
	g := gini.New()

	// Convert circuit to CNF
	b.c.ToCnf(g)

	// Add mutex constraints
	b.addMutexClauses(g)

	// Add the formula as an assumption
	g.Assume(formula)

	// Solve
	result := g.Solve()
	return result == 1 // 1 = satisfiable
}

// CheckAcceptSatisfiability checks if the accept field is satisfiable.
// Returns an error if the schema cannot accept any value.
func CheckAcceptSatisfiability(schema *Schema) error {
	if schema == nil {
		return nil
	}

	// Get the accept constraint
	accept := schema.Accept
	if accept == nil {
		return nil // no constraint = accepts everything
	}

	// Build definitions map
	definitions := make(map[string]*ir.Node)
	if schema.Define != nil {
		for name, node := range schema.Define {
			definitions[name] = node
		}
	}

	// Check each definition reachable from accept for cycles
	reachable := findReachableDefinitions(accept, definitions)

	for defName := range reachable {
		builder := newFormulaBuilder(defName, definitions)
		def := definitions[defName]
		if def == nil {
			continue
		}
		formula := builder.build(def)
		if builder.err != nil {
			return fmt.Errorf("error building formula for definition %q: %w", defName, builder.err)
		}
		if !builder.checkSatisfiability(formula) {
			return fmt.Errorf("definition %q has impossible cycle: no escape hatch exists", defName)
		}
	}

	// Also check the accept field itself
	builder := newFormulaBuilder("", definitions)
	formula := builder.build(accept)
	if builder.err != nil {
		return fmt.Errorf("error building formula for accept: %w", builder.err)
	}
	if !builder.checkSatisfiability(formula) {
		return fmt.Errorf("schema accept constraint is unsatisfiable: no value can match")
	}

	return nil
}

// findReachableDefinitions finds all definitions reachable from a node
func findReachableDefinitions(node *ir.Node, definitions map[string]*ir.Node) map[string]bool {
	reachable := make(map[string]bool)
	visited := make(map[string]bool)

	// Build index from base name to full definition name
	defIndex := make(map[string]string)
	for defName := range definitions {
		baseName, _ := ParseDefSignature(defName)
		defIndex[baseName] = defName
	}

	// Helper to find definition by ref content and mark as reachable
	findAndMark := func(refContent string) string {
		baseName, _ := ParseDefSignature(refContent)
		if visited[baseName] {
			return ""
		}
		visited[baseName] = true

		// Find the full definition name
		if fullName, ok := defIndex[baseName]; ok {
			reachable[fullName] = true
			return fullName
		}
		// Try exact match
		if _, exists := definitions[refContent]; exists {
			reachable[refContent] = true
			return refContent
		}
		return ""
	}

	var processNode func(n *ir.Node)
	processNode = func(n *ir.Node) {
		if n == nil {
			return
		}

		n.Visit(func(child *ir.Node, isPost bool) (bool, error) {
			if isPost {
				return true, nil
			}

			// Check for reference in string value: .[name] or .[name(args)] format
			if child.Type == ir.StringType {
				if strings.HasPrefix(child.String, ".[") && strings.HasSuffix(child.String, "]") {
					refContent := child.String[2 : len(child.String)-1]
					if fullName := findAndMark(refContent); fullName != "" {
						if def, ok := definitions[fullName]; ok {
							processNode(def)
						}
					}
				}
			}

			return true, nil
		})
	}

	processNode(node)
	return reachable
}
