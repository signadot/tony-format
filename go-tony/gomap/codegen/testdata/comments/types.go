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
