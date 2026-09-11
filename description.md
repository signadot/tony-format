# tony: is a whitespace-only line extraneous indentation? docs/tony.md says both, and the parser refuses it only before the first value

A question to resolve, not yet a defect: docs/tony.md answers it both ways, and the
parser takes a different side depending on where the line is. Raised by the go-tony docs
sweep (3d7t1khxh12krsddmdn0); measured at 546bd53.

## What the spec says

- White Space > Extraneous Indentation (docs/tony.md:941-947): "Tony disallows
  _extraneous indentation_ which is any leading whitespace of a line that does not have
  associated content, where content includes comments." A spaces-only line is exactly
  that. Indentation (:916) says the same: "disallows indentation which is not followed by
  a value or comment."
- The reader/writer paragraph (docs/tony.md:62-66, from 97e663b): "What a reader ACCEPTS
  is deliberately wider than what a writer PRODUCES ... a line holding nothing but
  whitespace ... [is] read without complaint."

## What the parser does

    "a: 1\n   \nb: 2\n"   accepted    (mid-document)
    "   \n"               (nil, nil)  (a document of only whitespace)
    "\t\n# c\n"           accepted
    "   \n# c\n"          refused: imbalanced document: extraneous indent
    "   \na: 1\n"         refused (Tony; YAML mode: "trailing material")

The tokenizer emits indent "   " then indent "" for a spaces-only line
(token/tokenize.go:28 at document start, and the newline handler after it); Balance
accepts that pair after content and refuses it before the first value;
parse.allBlank covers only a body that is entirely blank. Reachable from `o`:
`printf '   \na: 1\n' > f.tony; o view f.tony` exits 2. Wire traffic is unaffected: the
stream decoder skips indent tokens.

## To decide

- Extraneous Indentation wins: the reader/writer paragraph's "a line holding nothing
  but whitespace" is wrong, and the mid-document and whitespace-only acceptances are the
  bugs.
- The reader/writer paragraph wins: the refusal before the first value is the bug (the
  tokenizer is where a blank line stops reporting an indent), and Extraneous Indentation
  says a whitespace-only line has no indentation to be extraneous.

Either way, one of the two passages changes and the parser follows it everywhere.