package ir

const (
	IntKeysTag = "!sparsearray"
	IntKeysFmt = "%d"
	MergeKey   = "<<"
	// KeyTag marks an array as a keyed list, its argument naming the field
	// which keys the elements: !key(name).  A (key) path segment names an
	// element of such a list.
	KeyTag = "!key"
)
