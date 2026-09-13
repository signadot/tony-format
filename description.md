# o m -each should compose

`o m a -each | o m b -each` should be equivalent to `o '!and [a b]' -each`

the thing blocking this currently is the choice of the form of input vs output of
`o m -each`.

the proposed solution is to have `o m -each` output a single document which is a list
of matched documents from the input, coalesced from all input docs (eg --- separated).

optionally, it may add a comment to each element of the output showing its source
(eg '# input a.tony doc 3 element 4' )