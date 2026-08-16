# parse: a `---` section holding only comments is refused as an empty document, while a blank one is fine

go-tony v0.0.147, ParseMulti.

## The inconsistency

A multi-document stream whose middle section holds only prose:

    a: 1
    ---
    # only prose here
    ---
    b: 2

    parse.ParseMulti -> 0 docs, err=imbalanced document: empty document

Take the comment out and leave the section blank, and it is accepted:

    a: 1
    ---

    ---
    b: 2

    parse.ParseMulti -> 2 docs, err=<nil>

A trailing separator is accepted too (`a: 1\n---\nb: 2\n---\n` -> 3 docs). So a section with
NOTHING in it is a document that can be skipped, and a section with a COMMENT in it is an error.
Adding prose to an empty section turns an accepted file into a rejected one, which is the
opposite of what "a comment is not data" implies everywhere else.

ParseComments is not the variable — both settings refuse it identically. With
ParseComments(true) it is arguably stranger, because then the section does have something to
represent: a comment node with no value.

The error also names nothing an author can act on. It carries no offset, no line, and no
quotation of the source, unlike the parser's other refusals — so on a large file the first move
is bisecting it by hand.

## The library's own CLI accepts the same bytes

    $ o v empty-mid.tony
    a: 1

    ---
    b: 2

which prints the two documents and drops the prose section. I have not checked which entry
point `o` takes; from outside, the same file reads fine through one front end and errors
through the other.

## Why it costs something

verse charters are prose-dense — a rule, then a , then a paragraph explaining the rule
after it. A section of design notes standing between two rules is a natural thing to write, and
it makes the WHOLE file unreadable to , not just that section. One file in
that repo (the merge-gate half of docs/working/comment-drift.tony) could never be installed from
its own bytes for this reason, and nobody noticed because the rules had been installed
one at a time.

## Either behaviour would do

Skip it as the blank section is skipped, or — under ParseComments(true) — return it as a
document holding the comment. What is hard to work with is the third thing: refusing the file
and not saying where.