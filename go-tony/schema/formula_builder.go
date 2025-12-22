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
	definitions map[string]*ir.Node
	err         error               // first error encountered
}

// newFormulaBuilder creates a new formula builder for checking a definition
func newFormulaBuilder(checkingDef string, definitions map[string]*ir.Node) *formulaBuilder {
	return &formulaBuilder{
		c:           logic.NewC(),
		path:        "",
		vars:        make(map[varDef]z.Lit),
		mutexes:     make(map[string][]z.Lit),
		checkingDef: checkingDef,
		definitions: definitions,
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
		// Check if it's a reference like ".node"
		if strings.HasPrefix(node.String, ".") {
			refName := extractRefName(node.String)
			if refName != "" {
				return b.buildRef(refName)
			}
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

	// Handle reference tags like ".node" or ".array(.node)"
	if strings.HasPrefix(head, ".") {
		refName := extractRefName(head)
		if refName != "" {
			return b.buildRef(refName)
		}
		b.err = fmt.Errorf("invalid reference tag: %s", head)
		return b.c.F
	}

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

	default:
		b.err = fmt.Errorf("unknown tag in schema: %s", head)
		return b.c.F
	}
}

// buildRef handles definition references
func (b *formulaBuilder) buildRef(refName string) z.Lit {
	// Self-reference → constant false
	if refName == b.checkingDef {
		return b.c.F
	}

	// Expand inline
	if def, ok := b.definitions[refName]; ok && def != nil {
		return b.build(def)
	}

	// Unknown reference - error
	b.err = fmt.Errorf("unknown definition reference: .%s", refName)
	return b.c.F
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

	var processNode func(n *ir.Node)
	processNode = func(n *ir.Node) {
		if n == nil {
			return
		}

		n.Visit(func(child *ir.Node, isPost bool) (bool, error) {
			if isPost {
				return true, nil
			}

			// Check for reference in tag
			if child.Tag != "" && strings.HasPrefix(child.Tag, ".") {
				refName := extractRefName(child.Tag)
				if refName != "" && !visited[refName] {
					visited[refName] = true
					reachable[refName] = true
					if def, ok := definitions[refName]; ok {
						processNode(def)
					}
				}
			}

			// Check for reference in string value
			if child.Type == ir.StringType && strings.HasPrefix(child.String, ".") {
				refName := extractRefName(child.String)
				if refName != "" && !visited[refName] {
					visited[refName] = true
					reachable[refName] = true
					if def, ok := definitions[refName]; ok {
						processNode(def)
					}
				}
			}

			return true, nil
		})
	}

	processNode(node)
	return reachable
}
