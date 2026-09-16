# logd: a wildcard level is enumerated by building the container, so a set read is refused on exactly the containers a listing is for

A set read answers a level by enumerating the children of the node above it, and setChildren (server/session_read_set.go:359) does that by reading the node: readValueAt, which collects it under the session read budget. So `jobs.*` builds `jobs`.

That makes the budget the ceiling on a LISTING, and the listing was the thing that was supposed to escape it. session.md says a spec with no body "reads no node at all", which is true of the members and false of the container they are in. With DefaultReadBudget at 64 MiB nobody notices; set it small, or grow a container, and a listing is refused with `read exceeds its budget` -- the answer a listing exists to give instead.

Measured from verse, whose --paths now asks the store for addresses: 200 records of ~150 bytes under one container, a read budget of 8 KiB, and both the read of the container and the listing of it are refused with the same error.

## What the enumeration needs

The names of one level, and nothing else. Two ways that do not build the node:

- the INDEX: Index.Children (storage/index/index.go:17) is a map of the names beneath a path, which is exactly a level of an object; storage/index/cursor.go walks it already. It knows written paths rather than the value at a commit, so it answers for an object level and defers to the document where a segment is keyed or indexed -- which is what provenAbsent already says (storage/cursor.go:110-119).
- the EVENT STREAM: encodedMatch (server/session_read.go:132) already answers a read from the store event stream without building a node. A walk that takes the keys as they pass and drops the values is the same machinery with a narrower reader.

Either way the members keep coming one at a time, which is the part that already works.

## Why it was left

Deliberately, in the commit that built the set read: "Correctness first; the index is an optimization with its own issue if enumeration shows up in a profile" (session_read_set.go). It has shown up, but not as a profile -- as a refusal, on the case the feature is for.

## Not the same as paging

limit/cursor bounds how many members an ANSWER carries. This is about what the server holds to produce them: a page of ten members of a ten-thousand-member container still builds the container first.