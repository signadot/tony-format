package ir

import "strings"

// TagTree represents a parsed tag as a linked list of components.
// Args are themselves TagTrees to support nested tags like !array(array(int)).
//
// Example: !foo(a,b).bar(x) parses to:
//
//	TagTree{Name: "foo", Args: [{Name:"a"}, {Name:"b"}], Next: &TagTree{Name: "bar", Args: [{Name:"x"}]}}
//
// Example: !array(array(int)) parses to:
//
//	TagTree{Name: "array", Args: [{Name: "array", Args: [{Name: "int"}]}]}
type TagTree struct {
	Name string
	Args []*TagTree
	Next *TagTree
}

// ParseTag parses a tag string into a TagTree.
// Returns nil for empty tags.
func ParseTag(tag string) *TagTree {
	if tag == "" {
		return nil
	}

	head, args, rest := TagArgs(tag)

	// Strip leading ! from head
	name := head
	if len(name) > 0 && name[0] == '!' {
		name = name[1:]
	}

	tree := &TagTree{
		Name: name,
	}

	// Parse args as TagTrees (they don't have leading !)
	if len(args) > 0 {
		tree.Args = make([]*TagTree, len(args))
		for i, arg := range args {
			// Parse arg as a tag (prepend ! for parsing, then it gets stripped)
			tree.Args[i] = ParseTag("!" + arg)
		}
	}

	// Parse the rest recursively
	if rest != "" {
		tree.Next = ParseTag(rest)
	}

	return tree
}

// String reconstructs the tag string from the tree.
// Returns empty string for nil tree.
func (t *TagTree) String() string {
	if t == nil {
		return ""
	}

	var b strings.Builder
	t.writeTo(&b, true)
	return b.String()
}

func (t *TagTree) writeTo(b *strings.Builder, first bool) {
	if first {
		b.WriteByte('!')
	} else {
		b.WriteByte('.')
	}
	b.WriteString(t.Name)

	if len(t.Args) > 0 {
		b.WriteByte('(')
		for i, arg := range t.Args {
			if i > 0 {
				b.WriteByte(',')
			}
			arg.writeArgTo(b)
		}
		b.WriteByte(')')
	}

	if t.Next != nil {
		t.Next.writeTo(b, false)
	}
}

// writeArgTo writes the tag as an argument (no leading !, no dot separator)
func (t *TagTree) writeArgTo(b *strings.Builder) {
	if t == nil {
		return
	}

	b.WriteString(t.Name)

	if len(t.Args) > 0 {
		b.WriteByte('(')
		for i, arg := range t.Args {
			if i > 0 {
				b.WriteByte(',')
			}
			arg.writeArgTo(b)
		}
		b.WriteByte(')')
	}

	// Args shouldn't have Next (they're not chained with .)
	// but if they do, we'd need to handle it
}

// Map applies a function to each component's name recursively, returning a new TagTree.
// The function receives the component name and returns the new name.
// Args are also mapped recursively.
// Useful for parameter substitution in schema instantiation.
func (t *TagTree) Map(f func(name string) string) *TagTree {
	if t == nil {
		return nil
	}

	result := &TagTree{
		Name: f(t.Name),
	}

	// Map args recursively
	if len(t.Args) > 0 {
		result.Args = make([]*TagTree, len(t.Args))
		for i, arg := range t.Args {
			result.Args[i] = arg.Map(f)
		}
	}

	if t.Next != nil {
		result.Next = t.Next.Map(f)
	}

	return result
}

// Clone creates a deep copy of the TagTree.
func (t *TagTree) Clone() *TagTree {
	if t == nil {
		return nil
	}

	result := &TagTree{
		Name: t.Name,
	}

	if len(t.Args) > 0 {
		result.Args = make([]*TagTree, len(t.Args))
		for i, arg := range t.Args {
			result.Args[i] = arg.Clone()
		}
	}

	if t.Next != nil {
		result.Next = t.Next.Clone()
	}

	return result
}

// Len returns the number of components in the tag chain.
func (t *TagTree) Len() int {
	if t == nil {
		return 0
	}
	return 1 + t.Next.Len()
}

// Last returns the last component in the tag chain.
func (t *TagTree) Last() *TagTree {
	if t == nil {
		return nil
	}
	if t.Next == nil {
		return t
	}
	return t.Next.Last()
}
