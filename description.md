# docs: the MergeOps table omits 13 operators and documents one that does not exist

`o m -tags` and `o patch -tags` list what the binary will accept. The MergeOps table in
docs/matchpatch.md lists something else. Diffed:

    registered, absent from the table:
      addtag arraydiff at embed has-path insert irtype let rename replace retag rmtag strdiff

    in the table, registered nowhere:
      type

Thirteen operators a document may contain are undocumented, and one that is documented does
not exist.

## Why !at in particular

It answers a question people ask constantly and currently give up on: filter DOCUMENTS by a
condition somewhere inside them.

    o m '!at(items) !subtree {state: closed}' doc.tony

That works today and has all along. It came up while designing `o list -if` (ahy0ewwch12kr4n0fxn0):
a flag `o m -at path` was proposed, and rejected precisely because the operator already exists --
its only problem is that nobody can find it. An operator nobody can find is one that gets
proposed again as a flag.

mergeop/at.go carries a 20-line doc comment covering the path grammar, what a path naming nothing
does, wildcards, keyed segments, and how composition reads on either side of the walk
(!not.at(a.b) 3 against !at(a.b) !not 3, which are different questions). None of it is published.

## The `type` row is worse than a gap

It says "match by type", and no operator is registered under that name. A tag naming no operator
is not an error anywhere: mergeop.SplitChild folds it into the node as data, so a pattern written
from the table --

    accept: {age: !type number}

-- constrains nothing, matches nothing, and reports nothing. The operator meant is !irtype, which
takes an example VALUE whose node kind is the requirement rather than the name of a type:
!irtype "" for a string, !irtype 0 for a number.

That name was wrong in the schema context vocabulary too, and in three schema doc pages; fixed in
74d229b, which is where this table was noticed. mergeop/at.go's own doc comment still carries the
same mistake in its example (`!at(spec.replicas) !type int`).

## What would fix it

  1. the table matches `o m -tags` + `o patch -tags`. That diff is two commands and a sort, so it
     can be a test rather than a habit -- schema/match_vocabulary_test.go already does exactly
     this for the schema context vocabulary
  2. !at gets published: it has the documentation already, in at.go
  3. the type row becomes irtype, with the corrected example, and at.go's comment with it
  4. the diff-derived operators (insert, replace, delete, strdiff, arraydiff, rename, addtag,
     rmtag, retag) are listed as what a diff produces rather than left out. logd's storage
     vocabulary already documents which of them may be stored and why, at
     system/logd/api/storage_context.go, which is a better description than the table has for
     anything

## Not just tidiness

A pattern language people write by hand is only as large as its documentation. Thirteen missing
operators means thirteen things that get reimplemented as flags, worked around, or asked about --
and one documented operator that silently does nothing means a pattern that looks like a
constraint and is not.