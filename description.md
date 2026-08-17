# token: streaming scanners decided on truncated input in four more places, and the whole-document reader grew keep-chomped values

Looking for more of 75g1kbpdh12krs09gdn0 (a literal's bracket balance judged on a
scan the buffer cut off). The class is: a scanner meeting its construct cut in half
answers "is this valid?" when the only answerable question is "may more come?".

Found by sweeping every .tony document in the tree through the streaming path at
read sizes 1..4097 and comparing the tokens to the whole-document tokens. 73 of 106
documents differed at some size before this; 0 after.

Four more instances, all in TokenizeOne:

1. The document separator PANICKED. `---` plus the newline which ends it is four
   bytes; the guard established only that the third dash was in the buffer, then
   sliced four. `a: 1\n---\nb: 2\n` in 4-byte reads: slice bounds out of range.
   With spare capacity it instead read a byte that had not arrived.

2. The same separator, read in 3-byte chunks, came back as a LITERAL `---`: two
   documents silently merged into one, no error anywhere. Worse than the panic.

3. A buffer ending at a newline decided the next line's indent from nothing, so
   one-byte reads consumed the newline and left the separator to be scanned as a
   literal. The indent is the structure: reported short, a line lands at the wrong
   depth.

4. A merge key `<<` split across reads reported ErrUnterminated as a verdict.

5. A block-style string -- the quoted strings at one indent, taken together --
   came back as two separate strings when the read ended between them. nextMString
   answered "nothing follows" when it had merely run out of data. Two values where
   the document has one, silently.

And one that is not about streaming at all, surfaced by the sweep:

6. NewTokenizerFromBytes appended a trailing newline unconditionally, including to
   documents which already ended in one. A keep-chomped block scalar preserves it:
   `k: |+\n  x\n` read as "x\n\n", the encoder wrote that back, and the next read
   grew it again -- "x\n\n\n\n", then "x\n\n\n\n\n\n". Round-tripping a document
   corrupted it, unboundedly. The streaming path had always added the newline only
   when it was missing.

   Removing it exposed that an empty literal at the very end of a document
   (`k: |\n`) was only ever scanned because of that appended newline; scanLines
   now reads end-of-data after the `|` line as the empty literal it is.

Tests: token/streaming_boundary_test.go pins each instance across every split of a
small document, plus TestEverySplitReadsLikeTheWholeDocument, which is the sweep
itself over the tree's documents -- a scanner added later gets the same treatment
for free. parse/keep_literal_test.go round-trips a kept literal four times.