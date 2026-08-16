# ir: kpath {n} navigation expects a dense array, so a sparse index never resolves

`ir`'s kpath navigation treats a `{n}` segment as a dense array index, which a sparse array is
not: it is an object with integer keys.

```go
// ir/kpath.go, getKPath
if kp.SparseIndex != nil {
	// Sparse array handling - for now, treat as regular array index
	// This might need adjustment when sparse arrays are fully implemented
	if res.Type != ArrayType {
		return nil, fmt.Errorf("expected array for sparse index, got %s", res.Type)
	}
```

So `{n}` never resolves against a real sparse array:

```
$ cat sparse.tony
v: !sparsearray
{
  3: a
  7: b
}
$ o get 'v{7}' sparse.tony
error executing get: expected array for sparse index, got Object
```

The comment says as much -- it is a placeholder, not a bug that crept in. It was unreachable
from the CLI until `o get`/`o list` started taking kpaths, and reachable from logd only where
a sparse write is made, which is why nothing has tripped over it.

`ListKPath` has the same branch and needs the same answer. `ToIntKeysMap` and `FromIntKeysMap`
are how the rest of the code reads an integer-keyed object, so the shape is settled; this is
the one walk that does not use it.