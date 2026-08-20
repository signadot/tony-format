# docd: every write to an array element was refused, and logd could not read one

From Scott, reproduced against a real docd-backed verse and then here:

    verse d -put 0 scott test a '[2]'            -> non-field segment "[2]"
    verse d -put '"z"' scott test v votes '[0]'  -> non-field segment "[0]"

Every write to an array element failed through docd, mounted or not, whether or not the array
was there. Reads worked only because verse reads at the entity path and narrows in Go.

TWO FAULTS, one on each side.

docd refused the path before routing. splitPatch asks pathFields for the client's path, which
errors on any non-field segment, because decomposition nests the data under the path's FIELD
segments to partition the tree by mount. So a path holding an index was refused wherever the
index sat -- and nothing about mounts was involved, which is why it failed on base paths too.

The refusal was never necessary: a mount path is field-only (registration enforces it), so no
mount can be rooted at or below an index. A path holding one therefore has exactly ONE owner
-- the deepest mount over its field prefix, or base -- and needs no decomposition at all.
docd now routes by the field prefix (fieldPrefix), and a patch addressing inside a value is
routed whole rather than split.

logd could not read one either. extractPathValue walked object fields only, so a read at
a.votes[0] was refused as a bad segment -- while `o get 'a.votes[1]'` had always worked,
because ir walks indices, sparse indices and keys perfectly well. The server had its own
navigation with a TODO on it since before any of this. It delegates the step to ir now and
keeps its own error classification.

THE CLASSIFICATION, which the fix forced open. An index into an object had been reported as a
malformed path. It is not: it is a path the current state refutes, and the reasoning that put
it there -- "can never resolve" -- is not a property of a path in a mutable document. a.b[0]
resolves the moment someone writes an array at a.b, exactly as a.b.c does when someone writes
an object. So the taxonomy is now three facts about the PRESENT:

    not_found      nothing there; nothing contradicts the path, so creating is reasonable
    path_conflict  something there, of a shape which cannot hold it; creating means
                   clobbering, so stop and re-examine the shape you assumed
    invalid_path   not a well-formed question (a wildcard names a set, a read answers one)

path_conflict is new. It was being answered as not_found, so a client waiting for a value to
appear was waiting for one already there in a form it could not read.

AND THE PART THAT MAKES IT REAL. PathError.Is made EVERY kind satisfy ErrPathNotFound, so a
caller writing the obvious errors.Is(err, ErrPathNotFound) still read a conflict as absence --
the same collapse, one layer under the wire. It now matches absence only, and NoValueAt asks
"no value here, whatever the reason" on purpose rather than by accident.

Tests: TestArrayElementWriteAndRead writes and reads an element under a mount and on base, and
requires an element past the end to answer not_found. The classification tests hold the three
kinds to distinct codes and the sentinel to absence.