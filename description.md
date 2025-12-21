# diff ignores argument order - always diffs in same direction

## Bug

`o d a.tony b.tony` and `o d b.tony a.tony` produce identical output, but should be inverses.

## Reproduction

```
% cat a.tony b.tony
[ { a: 1} ]
[ { a: 1} { a: 2} ]

% o d a.tony b.tony
!arraydiff
1: !insert(bracket)
  a: 2

% o d b.tony a.tony
!arraydiff
1: !insert(bracket)
  a: 2    # BUG: should be !delete
```

## Expected

- `o d a.tony b.tony` (a→b): insert {a:2}
- `o d b.tony a.tony` (b→a): delete {a:2}

## Notes

The `-r` flag works correctly on the (wrong) result, suggesting the bug is in argument handling, not in the reverse logic.