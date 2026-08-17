package ir

// Navigation options.
//
// A comment wraps the value it precedes in a CommentType node, so a document
// parsed with comments has wrappers between containers and their contents.
// Navigation used to walk into one and stop: GetKPath("a") on "# lead\na: 1"
// answered "expected object, got Comment", GetPath the same, and ListPath
// panicked outright. Since the index, reads, scope-owned paths and watches are
// all path-addressed, one comment made a document unaddressable.
//
// So navigation sees through comments, and the option says what to do with the
// one at the END of the walk: by default the value is answered, since that is
// what a path names. WithComments keeps the wrapper, for a caller that came
// looking for what was said about the value rather than the value.
//
// The plumbing is here rather than at each call site because the format offers
// comments to diffs, patches and matches "if so desired", and each of those
// grew its own flag late and separately. This one exists before it is needed.
type NavOpt func(*navConfig)

type navConfig struct {
	// Comments keeps a comment wrapper on the node a walk answers with.
	Comments bool
}

// WithComments answers the node as it stands, comment and all, rather than the
// value the comment describes.
func WithComments(v bool) NavOpt {
	return func(c *navConfig) { c.Comments = v }
}

func navCfg(opts []NavOpt) navConfig {
	var cfg navConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// Uncomment answers the value a comment describes, at whatever depth comments
// nest. A node which is not a comment is itself.
//
// Every walk uses it before asking what kind of node it is standing on: a
// comment is not a container, and a question about structure is not a question
// about what was said.
func Uncomment(n *Node) *Node {
	for n != nil && n.Type == CommentType && len(n.Values) == 1 {
		n = n.Values[0]
	}
	return n
}

// answer applies the option to the node a walk arrived at.
func (c navConfig) answer(n *Node) *Node {
	if c.Comments {
		return n
	}
	return Uncomment(n)
}

// StripComments answers a tree with every comment removed: the wrappers a head
// comment makes and the line comment each node carries.
//
// It is what "without comments" means. Half of it used to happen by accident: a
// head comment is a wrapper and was discarded when something descended through
// it, while a line comment rides on the node and was carried along by every
// clone -- so a patch which dropped comments dropped only half of them, and a
// store which kept none kept some.
//
// It does not touch what it is given. It used to strip IN PLACE, and its one
// caller strips the RESULT of a patch, which shares its untouched subtrees with
// the document that was patched -- so tony.Patch, with comments off, reached back
// into the caller's document and took the comments out of it. That is the
// property head.go names and relies on ("it does not mutate the document it is
// given, so an earlier head stays valid for anyone still holding it"), and it held
// only because logd passes Comments(true) and so never reached this.
func StripComments(n *Node) *Node {
	if n == nil {
		return nil
	}
	return stripComments(n.Clone())
}

// stripComments is StripComments on a tree the caller owns.
func stripComments(n *Node) *Node {
	n = Uncomment(n)
	if n == nil {
		return nil
	}
	n.Comment = nil
	for i, f := range n.Fields {
		if stripped := stripComments(f); stripped != nil {
			stripped.Parent = n
			stripped.ParentIndex = i
			n.Fields[i] = stripped
		}
	}
	for i, v := range n.Values {
		if stripped := stripComments(v); stripped != nil {
			stripped.Parent = n
			stripped.ParentIndex = i
			n.Values[i] = stripped
		}
	}
	return n
}
