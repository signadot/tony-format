package mergeop

// Summaries are what each operation does, in one line.
//
// A Symbol knows its name and whether it matches or patches, and no more, so this is
// where the prose lives. It is here rather than only in the reference page because the
// tool has the same question to answer: `o patch -tags` lists the operations a patch may
// use, and a list of bare names tells a reader who does not already know them nothing.
//
// docs/generated/index.md carries the same summaries, and TestSummariesMatchTheIndexTable
// holds the two together, so the page and the tool cannot drift apart.
var summaries = map[string]string{
	"addtag":     "add a tag; the tag is what results",
	"all":        "apply the match (resp. patch) to every element of an array or object",
	"and":        "conjoin a list of matches, each applied to the corresponding doc",
	"arraydiff":  "an array edit, relative and positional",
	"at":         "walk to the path and apply the match there; see below",
	"comment":    "match or state the comments here; the operand names head, line or both, [] for none",
	"delete":     "remove a value; absence is what results",
	"dive":       "dive into the doc and treat each subtree with a list of matches/patches",
	"embed":      "the operand is the result, with each occurrence of the key replaced by the doc",
	"field":      "match the field (a string), not its value",
	"get-path":   "the node at a kpath: !get-path(root) spec.image",
	"glob":       "glob match a string",
	"has-path":   "the document has the path the operand names",
	"if":         "evaluate if: and patch with then: or else:",
	"insert":     "add a value; the value is what results",
	"ir":         "match the node's IR fields, not its value: !ir {int: .[number]} an integer",
	"irtype":     "the node's kind equals the operand's: !irtype \"\" a string, !irtype 0 a number",
	"json-patch": "apply a json patch to the corresponding doc node",
	"key":        "associative lists as objects",
	"let":        "bind names in let:, then match or patch with in:, referring to them as .[name]",
	"list-path":  "the nodes at a kpath as a list; takes the wild paths !get-path refuses",
	"not":        "negate a match (eg !not.or [1,2,3])",
	"nullify":    "turn a node into a null without deleting it",
	"or":         "disjunction",
	"pass":       "match: accept anything / patch: leave the document as it is",
	"pipe":       "pipe the doc node to a program and replace it with the program's output",
	"quote":      "quote a document as a string",
	"raw":        "the escape: treat the subtree as data, interpreting no operation at any depth",
	"rename":     "rename fields, relative to the keys that are there",
	"replace":    "verify the node still equals from:, then install to:",
	"retag":      "verify the tag is from, then make it to",
	"rmtag":      "remove a tag; its absence is what results",
	"strdiff":    "a string edit, relative to the string that is there",
	"subtree":    "match any subtree of the doc",
	"tag":        "match the tag of a node, not its value",
	"unquote":    "unquote a string as a document",
}

// Summarized is a Symbol which can say in one line what it does. A consumer's operation
// implements it to be described by `o patch -tags` alongside the built-in ones; without
// it the operation is still listed, with nothing beside it.
type Summarized interface {
	Summary() string
}

// Summary is the one-line description of an operation, by name, without the '!'.
//
// The built-in summaries are here; anything else is asked of the Symbol itself. It
// answers "" when neither has one, which is what a listing shows as an empty description
// rather than as an absent operation.
func Summary(name string) string {
	if s, ok := summaries[name]; ok {
		return s
	}
	if sym, ok := Lookup(name).(Summarized); ok {
		return sym.Summary()
	}
	return ""
}
