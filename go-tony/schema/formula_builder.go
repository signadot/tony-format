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

// defVariants holds the definitions a base name can have.  `foo` and `foo(t)`
// are two definitions, not one, and which of them a reference means is said by
// whether it carries arguments -- so an index which keeps one per base name
// answers with whichever the map iteration happened to write last.  It did:
// `accept: .[bar(int)]` passed a schema whose bar(t) nothing can satisfy about
// half the time, and rejected the satisfiable half of the other pair as often,
// in the same binary, on the same input.
type defVariants struct {
	plain string // "foo", if there is one
	param string // "foo(t)", if there is one
}

// pick answers the definition a reference with this many arguments means, and
// "" when there is no such definition.  A reference with arguments means the
// parameterized one; without, the plain one -- falling back to the
// parameterized body uninstantiated, which is what BuildDefEnv hands a zero-arg
// call.
func (v defVariants) pick(nArgs int) string {
	if nArgs > 0 {
		return v.param
	}
	if v.plain != "" {
		return v.plain
	}
	return v.param
}

// indexDefinitions groups definitions by base name, keeping both variants.
func indexDefinitions(definitions map[string]*ir.Node) map[string]defVariants {
	index := make(map[string]defVariants, len(definitions))
	for defName := range definitions {
		baseName, params := ParseDefSignature(defName)
		v := index[baseName]
		if len(params) == 0 {
			v.plain = defName
		} else {
			v.param = defName
		}
		index[baseName] = v
	}
	return index
}

// formulaBuilder builds a SAT formula from schema IR
type formulaBuilder struct {
	c           *logic.C
	path        string             // current kinded path position
	vars        map[varDef]z.Lit   // (position, type) → literal
	mutexes     map[string][]z.Lit // position → types seen (for mutex)
	checkingDef string             // definition being checked (self-ref → false)
	defParams   map[string]bool    // parameter names of current definition (e.g., "t" for list(t))
	visiting    map[string]bool    // definitions currently being visited (cycle detection)
	definitions map[string]*ir.Node
	defIndex    map[string]defVariants // base name → the definitions it names
	err         error                  // first error encountered
}

// newFormulaBuilder creates a new formula builder for checking a definition
func newFormulaBuilder(checkingDef string, definitions map[string]*ir.Node) *formulaBuilder {
	defIndex := indexDefinitions(definitions)

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
		// A bare null pattern matches anything -- that is the wildcard, and it
		// is why !not null matches nothing.  Reading it as the null TYPE is how
		// this checker came to pass base.tony's own int, whose {int: !not null}
		// clause can never hold.  !irtype null is the type question.
		return b.c.T
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
	head, args, rest := ir.TagArgs(tag)

	if ir.IsPresentation(head) {
		// Presentation tags select a rendering, not a value, so they do not
		// affect satisfiability: build from the content.
		child := node.Clone()
		child.Tag = rest
		return b.build(child)
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

	case "!ir":
		// The pattern is over the node's IR representation, which is a
		// different shape from the value this checker reasons about.  An
		// opaque proposition rather than a constant, so that a negation of it
		// stays satisfiable too: what !ir says is unknown here, in both
		// polarities, and a checker that invents a contradiction is worse than
		// one that misses it.
		return b.c.Lit()

	case "!all":
		// !all X asks two different things of two different places, which is the
		// walk allOp does: of a container, that every ELEMENT match X -- and an
		// empty container matches whatever X is, so the container case constrains
		// nothing at this position beyond being a container; of a scalar, that X
		// hold of the scalar ITSELF.
		//
		//	(array here ∨ object here) ∨ X here
		//
		// X was built at this position unconditionally, which asserted the
		// ELEMENT type of the node: `!and [!irtype [], !all.irtype 1]` -- a list
		// of numbers -- mutexed number against array and read as impossible,
		// while the same thing written `!all .[number]` was accepted, since a
		// payload with no tag chain left nothing to build. Two spellings, one
		// meaning, opposite verdicts.
		//
		// The element formula is not built at all: no assignment of it can make
		// the whole unsatisfiable, since the empty container satisfies any X, and
		// a checker which invents a contradiction is worse than one which misses
		// it. What survives is the scalar reading, which is a real constraint --
		// `!and [!irtype 1, !all.irtype ""]` asks a number to be a string.
		child := node.Clone()
		child.Tag = rest
		return b.c.Ors(b.getVar("array"), b.getVar("object"), b.build(child))

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
		//
		// The tag's arguments go with it: !person(int) names the parameterized
		// person, and dropping them referred to a definition of the same name
		// which may be a different one, uninstantiated.
		if _, ok := b.defIndex[tagName]; ok {
			refContent := tagName
			if len(args) > 0 {
				refContent += "(" + strings.Join(args, ",") + ")"
			}
			return b.buildRef(refContent)
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

	// Which definition this reference names: `foo` and `foo(t)` are two, and the
	// arguments say which.  An exact hit wins outright -- a schema may define
	// "foo(int)" as a name in its own right.
	fullDefName := refContent
	if _, ok := b.definitions[refContent]; !ok {
		variants, known := b.defIndex[baseName]
		if !known {
			b.err = fmt.Errorf("unknown definition reference: .[%s]", refContent)
			return b.c.F
		}
		fullDefName = variants.pick(len(refArgs))
		if fullDefName == "" {
			b.err = fmt.Errorf("%s takes no arguments: .[%s]", baseName, refContent)
			return b.c.F
		}
	}

	// Already inside the definition this resolves to: no escape through here.
	// Keyed by the definition, not by its base name, so a parameterized
	// definition may refer to the plain one of the same name -- which is how
	// array(t) is written.
	if b.visiting[fullDefName] {
		return b.c.F
	}

	def, ok := b.definitions[fullDefName]
	if !ok || def == nil {
		b.err = fmt.Errorf("definition body not found: %s", fullDefName)
		return b.c.F
	}

	b.visiting[fullDefName] = true
	defer delete(b.visiting, fullDefName)

	// If the definition is parameterized and the reference carries arguments,
	// instantiate it.  A parameterized definition referred to without arguments
	// is its body as written, which is what BuildDefEnv hands a zero-arg call.
	_, defParams := ParseDefSignature(fullDefName)
	if len(defParams) > 0 && len(refArgs) > 0 {
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
	// An object pattern asks for an object, and the fields it names are asked of
	// THAT object: without this the checker read {int: X} as a constraint on the
	// field alone, so !and [.[number], {int: X}] -- a node required to be both a
	// Number and an object -- looked satisfiable.  That is base.tony's own int
	// as it was written, and nothing matched it.
	return b.c.Ands(append(lits, b.getVar("object"))...)
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
	// implicit AND for piecewise matching, of an array: the same reading as
	// buildObject's, since an untagged array pattern matches only arrays
	return b.c.Ands(append(lits, b.getVar("array"))...)
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

	defIndex := indexDefinitions(definitions)

	// Helper to find definition by ref content and mark as reachable.
	//
	// Keyed by the definition a reference names, not by its base name: `foo` and
	// `foo(t)` are two definitions, and marking one of them because the other
	// was mentioned checked the wrong body -- half the time, whichever the map
	// kept.  A schema which refers to both now has both checked.
	findAndMark := func(refContent string) string {
		baseName, args := ParseDefSignature(refContent)

		fullName := ""
		if _, exists := definitions[refContent]; exists {
			fullName = refContent
		} else if variants, known := defIndex[baseName]; known {
			fullName = variants.pick(len(args))
		}
		if fullName == "" || visited[fullName] {
			return ""
		}
		visited[fullName] = true
		reachable[fullName] = true
		return fullName
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
