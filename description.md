# o m -trim !or [ ... ] is broken

When running Trim and the match document has a different type than the
input document, we create incorrect results

example

a: b
a: !or [ b c d ]
a: []