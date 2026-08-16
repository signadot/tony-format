package comments

// Spec carries the comments written about it: comment= names the field holding
// what was said ABOVE the value, lineComment= what was said after it. The
// annotations are the ones gomap's reflection mapper reads, so a type behaves
// the same whether its codecs are generated or reflected
// (issue 3cdjz00jh12krns4g1n0).
//
//tony:schemagen=comments-spec,notag,comment=Comments,lineComment=LineComments
type Spec struct {
	Replicas     int `tony:"field=replicas"`
	Comments     []string
	LineComments []string
}

// Doc holds a Spec, so the comments are on a nested value rather than the
// document root -- which is where a line comment has a line to end.
//
//tony:schemagen=comments-doc,notag
type Doc struct {
	Name string `tony:"field=name"`
	Spec *Spec  `tony:"field=spec"`
}

// Rule is an element of a charter's list. Each carries its own comment, which
// is what a struct-level annotation can reach.
//
//tony:schemagen=comments-rule,notag,comment=Comments
type Rule struct {
	Name     string `tony:"field=name"`
	Comments []string
}

// Charter is the shape the field-level annotation exists for: the comment above
// `rules:` -- and the one above the FIRST element, which the format attributes
// to the list because a block array begins at its first element -- land on the
// list itself. No struct is there to carry them, so the FIELD names somewhere
// (issue xvexrbthh12ksrahg5n0).
//
//tony:schemagen=comments-charter,notag
type Charter struct {
	Rules        []Rule `tony:"field=rules,comment=RulesComment,lineComment=RulesLine"`
	RulesComment []string
	RulesLine    []string

	// Gates is a carrier on a field that may not be written at all: omitzero
	// drops it when empty, and the comment has to be dropped with it rather
	// than assigned onto the nothing that is there.
	Gates        []string `tony:"field=gates,omitzero,comment=GatesComment"`
	GatesComment []string
}
