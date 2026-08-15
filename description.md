# match: comments cannot be matched, which the format says they should be

The format says tools support "diffs, patching, and matching comments if so desired"
(docs/tony.md, Comments). Patching them is now possible -- mergeop.Comments(true), 168e8ca.
Matching them is not: there is no way to ask, and no code that would answer.

## What exists

Matching is comment BLIND, as of 44914cd, and that is the right default: a comment describes a
value and is not what the value IS, so a document parsed with comments must answer a pattern the
same way as one without. Before that fix it was neither blind nor sighted -- `{name: svc}` simply
failed against "# lead\nname: svc", answering false with no error.

## What is missing

An option -- MatchComments(true), say, setting the same mergeop flag a patch sets -- and the code
it would turn on. I wrote the option, found it did not work, and removed it rather than ship it:
with comments participating, matchNode has no case that compares them, so two IDENTICAL comments
still mismatched. An option which silently does nothing is the thing this repository has spent a
week removing.

## The decisions it needs, which is why it is filed rather than guessed

  - what does a pattern's comment ASK of the document's? Exact lines, a glob, "these lines appear
    among yours", or the presence of any comment at all
  - is a pattern with no comment a statement that the document has none, or silence? Object
    fields are silent about what they do not name, which argues for silence -- but then a pattern
    can never ask for the ABSENCE of a comment
  - line comments and head comments are different things in the IR (node.Comment against a
    CommentType wrapper). One option for both, or separate
  - !glob and the other string operators over comment lines: does `# !glob "TODO*"` mean anything

## Where

match.go: matchNode unwraps both sides through uncomment() unless told otherwise; the option would
be a MatchOpt setting mergeop's Comments on the OpContext the walk already carries, which is what
patch reads.