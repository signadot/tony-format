package ir

const (
	IntKeysTag = "!sparsearray"
	IntKeysFmt = "%d"
	MergeKey   = "<<"
	// KeyTag marks an array as a keyed list, its argument naming the field
	// which keys the elements: !key(name).  A (key) path segment names an
	// element of such a list.
	KeyTag = "!key"

	// BracketTag asks that a subtree be written in bracketed style.  It is the
	// one degree of freedom the normalized form allows, so it records how a
	// node was written and not what it is.  See IsPresentation.
	BracketTag = "!bracket"
	// LiteralTag asks that a string be written as a block literal.  Like
	// BracketTag it selects a rendering for a value that is the same value
	// either way.  See IsPresentation.
	LiteralTag = "!literal"
)
