# kpath: ergonomic delimiters other than '.'

Typing a kpath whose field names contain dots is unpleasant, and there is no way
to ask for a different separator. This is the design space, with what the grammar
actually allows measured rather than assumed.

## What is there today

A field segment is separated by '.'; a field containing one of . [ { ( ' " must be
quoted:

    "example.com".tls."1.2.3"

That is correct and round trips. It is also what a person has to type, inside
whatever quoting their shell adds.

## The constraint: there is no free character

A bare field may contain ANYTHING except . [ { ( ' " -- so every candidate sigil
is already a legal path today:

    Parse("/a/b")   -> one field named "/a/b"
    Parse("|a|b")   -> one field named "|a|b"
    Parse("~a~b")   -> one field named "~a~b"
    Parse(`a\.b`)   -> two segments, "a\" and "b"   (backslash is literal)

So a leading-sigil scheme ("/users/example.com" meaning slash-separated) and a
\. escape both SILENTLY REINTERPRET paths that are valid now. The only
syntactically free prefixes are accidents of the error cases -- "[]", ".[", a
lone "." -- and they are ugly enough to be worse than the problem.

This is the fact that decides the design: paths cannot become self-describing
about their delimiter without narrowing what a bare field may contain, which is a
format change.

## Proposed

### 1. The delimiter is a parse/print option, never part of a stored path

    kpath.Parse("users/example.com/tls", kpath.Delim('/'))
    kp.StringWith(kpath.Delim('/'))   // display, echo, error messages
    kp.String()                        // canonical, always '.'

The invariant that makes this safe: ONE canonical spelling reaches storage, the
index, comparisons and the wire. That is not style. The snapshot reader compares
paths as strings (currentPath == desPathStr) and the index keys its children by
segment string, so two spellings of one path in stored data is the shape of the
lookup bug in gx8xvgmph12krbjpg1n0.

The delimiter belongs where the context is known and nowhere else: an `o` flag, a
docd mount config, an API request field.

It moves the pain rather than removing it -- with Delim('/'), a field containing
'/' needs quoting -- so it is a per-data-shape choice. Domains and versions want
'/', filesystem-ish keys want '.'.

One delimiter per parse. A SET of delimiters would make "a/b.c" ambiguous.

### 2. A builder API, so composition never goes through string concatenation

    kpath.Fields("example.com", "tls")
    kp.Append(kpath.Field(name), kpath.Index(0))

This is where the ergonomic damage actually was: Join read its prefix as a single
segment, so building a path a segment at a time collapsed everything before the
last step into one field, and docd mounted every three-deep path at the wrong
place (fixed in 7e5d98f). A builder that never renders an intermediate string
cannot have that class of bug.

### 3. For the CLI, sidestep the question where it is cheapest

    o get -k users -k example.com -k tls

A repeatable segment flag needs no delimiter, no quoting and no shell escaping.
-d/ for the compact form when a user prefers it.

## Not proposed, but the honest alternative

If paths should be self-describing -- readable without knowing which option
produced them, which is a real want given kpaths travel on the wire and live in
snapshots -- the price is reserving a character out of the bare-field grammar in
a new format version. After that "/a/b" can mean what it looks like. That is a
deliberate format decision, not something to slip in under an ergonomics ticket.

## Also worth deciding once

o's object paths ($.a.b, docs/objpath.md) are a second syntax with the same
question. Whatever is decided here should say what happens there.