# o eval: a file written for eval has to say !eval, and says nothing when it does not

## What happens

`o eval` resolves `!eval` tags. A document with no such tag is echoed back unchanged, which is
right -- the command must be safe over input it did not write, where a literal `$[x]` is the text
`$[x]` and only a tagged subtree expands.

It is also what someone writing a file FOR `o eval` trips over. The file is for eval, so the tag
reads as noise and is left out, and the command answers with the input, expanding nothing and
saying nothing about why.

## What is needed

A flag on `o eval` that evaluates the whole input, as if its root carried `!eval`, rather than
only the subtrees tagged with it. Its help line is what tells the confused author what happened:
"evaluate the whole input, not just subtrees tagged with !eval".

The default does not change: untagged input is untouched, which is what makes the command safe
over arbitrary documents.