# parse: an opt-in option to sort object keys

Parse keeps object keys in source order, and that is the default on purpose: `o view` writing a file, rename, trim, `-each` and logd's schema order all rely on it (a trial of sorting by default failed 7 tests across 5 packages).

Some callers want sorted keys anyway. Add an opt-in `parse.SortKeys()` that answers every string-keyed object with its fields in byte order of the key, the order `ir.FromMap` gives.

- An object holding a merge key is left in source order: a merge key is not a name, and where it sits is part of what it means.
- Int-keyed objects (`!sparsearray`) are already sorted by `FromIntKeysMapAt`.
- The sort runs after comments are associated, so a comment travels with the value it belongs to; the key and value nodes are moved, not rebuilt, so `ParsePositions` keeps pointing into the tree.