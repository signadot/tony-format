package matrix

// M is the combination matrix: every composition of the three container
// constructors -- pointer, list, map -- over a scalar and over a struct with a
// codec of its own, to two and three levels.
//
// The point of writing them all out is that codegen used to descend exactly one
// level, and a test that names a handful of shapes cannot tell the difference
// between "recursive" and "one level deep with a well-chosen example".
//
//tony:schemagen=matrix-m,notag
type M struct {
	// One level: what already worked.
	Sl    []string          `tony:"field=sl,omitzero"`
	Mp    map[string]string `tony:"field=mp,omitzero"`
	PtrSl *[]string         `tony:"field=ptrSl,omitzero"`
	PtrLf *Leaf             `tony:"field=ptrLf,omitzero"`
	SlLf  []Leaf            `tony:"field=slLf,omitzero"`
	MpLf  map[string]Leaf   `tony:"field=mpLf,omitzero"`

	// Two levels.
	SlSl   [][]string                   `tony:"field=slSl,omitzero"`
	SlMp   []map[string]string          `tony:"field=slMp,omitzero"`
	MpSl   map[string][]string          `tony:"field=mpSl,omitzero"`
	MpMp   map[string]map[string]string `tony:"field=mpMp,omitzero"`
	PtrMp  *map[string]string           `tony:"field=ptrMp,omitzero"`
	SlPtr  []*Leaf                      `tony:"field=slPtr,omitzero"`
	MpPtr  map[string]*Leaf             `tony:"field=mpPtr,omitzero"`
	SlSlLf [][]Leaf                     `tony:"field=slSlLf,omitzero"`
	MpSlLf map[string][]Leaf            `tony:"field=mpSlLf,omitzero"`

	// Three levels, including a pointer in the middle and at the bottom.
	SlSlSl   [][][]string                   `tony:"field=slSlSl,omitzero"`
	SlMpSl   []map[string][]string          `tony:"field=slMpSl,omitzero"`
	MpSlMp   map[string][]map[string]string `tony:"field=mpSlMp,omitzero"`
	PtrSlMp  *[]map[string]string           `tony:"field=ptrSlMp,omitzero"`
	PtrMpSl  *map[string][]string           `tony:"field=ptrMpSl,omitzero"`
	SlPtrSl  []*[]string                    `tony:"field=slPtrSl,omitzero"`
	MpPtrSl  map[string]*[]string           `tony:"field=mpPtrSl,omitzero"`
	MpSlPtr  map[string][]*Leaf             `tony:"field=mpSlPtr,omitzero"`
	PtrSlPtr *[]*Leaf                       `tony:"field=ptrSlPtr,omitzero"`

	// A pointer to a pointer. Nothing in the format distinguishes the two
	// levels -- both are null when nil -- but the Go type says it, and a codec
	// that will not generate for it is a codec that says no to a legal struct.
	PP   **string `tony:"field=pp,omitzero"`
	PPLf **Leaf   `tony:"field=ppLf,omitzero"`

	// Fixed-size arrays, which are not slices: [N]T cannot be made, and a
	// document holding more than N elements is an error rather than a silent
	// truncation.
	Ar     [2]string   `tony:"field=ar,omitzero"`
	SlAr   [][2]string `tony:"field=slAr,omitzero"`
	ArSl   [2][]string `tony:"field=arSl,omitzero"`
	PtrAr  *[2]string  `tony:"field=ptrAr,omitzero"`
	ArLeaf [2]Leaf     `tony:"field=arLeaf,omitzero"`

	// An array slot is always written -- there is no absent slot -- so a nil
	// slice in one comes back empty. A slot of pointers is what keeps that
	// distinction, the same answer as everywhere else.
	ArPtrSl [2]*[]string `tony:"field=arPtrSl,omitzero"`

	// A named string key: legal, and a map[Key]V cannot be indexed with a
	// plain string.
	MpKey   map[Key]string   `tony:"field=mpKey,omitzero"`
	SlMpKey []map[Key]string `tony:"field=slMpKey,omitzero"`
	MpKeySl map[Key][]string `tony:"field=mpKeySl,omitzero"`
}

// Key is a named string, which is a legal map key and not the same type as
// string.
type Key string

// Leaf is a struct with a codec, so the recursion has to stop at a codec call
// as well as at a scalar.
//
//tony:schemagen=matrix-leaf,notag
type Leaf struct {
	Name string `tony:"field=name"`
}
