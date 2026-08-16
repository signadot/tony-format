# mergeop: arraydiff cannot insert into an absent or null array, so an array cannot be created by inserting its first element

`!arraydiff` refuses a base that is not already an array, so the first element of an array
cannot be written by inserting it. An empty array accepts the same insert.

## Reproduce

```
doc {v: []}      patch {v: !arraydiff {0: !insert 99}}  ->  {v: [99]}
doc {v: null}    patch {v: !arraydiff {0: !insert 99}}  ->  ERROR arraydiff only applies to arrays, got Null at $.v
doc {other: 1}   patch {v: !arraydiff {0: !insert 99}}  ->  ERROR arraydiff only applies to arrays, got Null at $
```

(`mergeop/arraydiff.go:47`)

## Why it matters

An object field is created by writing it -- nothing has to exist first. An array element
cannot be, so a client that wants `v[0]` has to know whether `v` exists yet and switch
spellings: whole-array write to create, insert to extend. That is state the writer often does
not have, and getting it wrong is not a no-op -- through logd it poisons the log
(7cdvym1fh12ksmd5g5n0).

## Shape of a fix

An insert-only arraydiff against a null or absent base can read as an insert into `[]`, which
is what the empty-array case already does. A base that is a non-null, non-array value is a
different question and should stay an error.

Seen on main at 30817c5.