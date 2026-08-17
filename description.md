# yaml: every plain scalar meeting a read boundary was chopped there, and an unterminated double-quoted string was accepted

Continuing the sweep from pdjqd3nqh12ks69ggdn0, widened to split POSITIONS (two
reads cut at each byte in turn, not just fixed read sizes) and to the other formats
the tokenizer reads. Tony is clean at every position of every document in the tree.
JSON is clean. YAML was not: 80 of 94 documents in the tree tokenized differently
when read in chunks.

1. Plain scalars. yamlPlain ends its scan at the end of the data and returns, so a
   scalar which runs to the buffer end was read as complete. Every scalar meeting a
   read boundary was chopped there, silently:

       string  ->  str
       true    ->  four literals, which is not a boolean
       44      ->  two 4s
       match:  ->  a string with the colon inside it

   testdata/crds.yaml, 3.6MB, read at 4096 bytes -- an ordinary pipe read -- lost a
   token at every boundary that fell inside a scalar. `string` came back as `str`
   at token 245.

2. Folded scalars. A plain scalar continues onto the next line when that line is
   indented far enough, and yamlPlain looks ahead to see. When the lookahead ran
   into the end of the buffer it concluded the scalar ended, cutting
   `pre\n  hello\n  post` -- one value -- into several tokens.

3. `:9091`. A ':' with no space after it is part of a plain scalar, and the byte
   after it says so. A buffer ending at the colon cannot tell, and TokenizeOne
   called it a key separator.

4. Quoted strings. A single-quoted string split across reads reported
   ErrUnterminated as a verdict rather than asking for the rest.

5. And one that is not about streaming: an unterminated DOUBLE-quoted string was
   accepted. The closing quote is the only way out of that scan, so falling out of
   it means there was none -- but it returned the token anyway, so
   `a: "no closing quote` parsed with the rest of the line as its value. Single
   quotes reported it; double quotes did not.

All of it is the class 75g1kbpd was an instance of: a scanner meeting half a
construct answers "is this valid?" where the only answerable question is "may more
come?".

Fixed by giving yamlPlain a ranOut result -- the scan reached the end of the data
rather than the end of the scalar -- and having the tokenizer turn that into io.EOF
while more data can arrive, which is the idiom the rest of the file already uses.

Tests: TestEverySplitReadsLikeTheWholeDocument now covers .tony, .yaml and .json
documents in the tree, and TestEverySplitPositionReadsLikeTheWholeDocument cuts
every document under 2KB at every byte in turn. TestUnterminatedQuotedYAMLIsAnError
pins the double-quote verdict.