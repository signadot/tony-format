# tags: an argument has no quoting, so a name holding a comma or ')' does not survive one

Found while sweeping for more of r05ms7nch12ksxttgdn0 (a name rendered into a
structured string without the quoting it needs stops being a name). Paths are fixed;
tag arguments have the same shape and no quoting at all.

    ir.TagCompose("!key", []string{"has,comma"}, "")  ->  !key(has,comma)
    ir.TagArgs("!key(has,comma)")                     ->  args ["has" "comma"]

    "has)close"  ->  !key(has)close)   ->  args ["has"]
    ""           ->  !key()            ->  args []

Every other awkward name survives: dots, braces, brackets, quotes, spaces, unicode,
"*".  Only a comma, a ")" and the empty name do not.

WHERE IT IS LIVE. scope_overlay.go builds `!key(field)` from a field NAME:

    list.Tag = ir.TagCompose("!key", []string{field}, "")

so a keyed list whose key field is named with a comma or a paren gets a tag which
says something else -- two key fields, or a truncated one. The name comes from a
schema declaration rather than from data, which is why this is filed rather than
urgent: nobody writes `key: has,comma` by accident, where an ENTITY ID with a dot in
it (`git:ref:tony/v0.0.158`) arrives from the world.

WHAT IT WOULD TAKE. Tag arguments have no quoting syntax, so this is a grammar
decision: either give them one (and teach TagArgs, TagCompose, ParseTag and the
encoder about it), or state that a tag argument is a restricted identifier and have
whoever builds one from a name refuse the names that do not fit. The second is
smaller and might be the honest description of what a tag argument is.

Not urgent, but it is the same class as the path bug, and the same class is what
left 192 entities standing.