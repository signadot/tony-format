# API

## Scopes

See [SCOPES.md](../storage/docs/SCOPES.md) for documentation on COW (copy-on-write) scope isolation.

## Kinded Paths

### ArrayLike

```
a: # a is array like
- b
- c
---
a: # a is array like
0: b
4: c
---
a: [] # a is array like
---
{ 0: a }  # array like


```
### Non-Array-Like

```
1 # not array like
---
null
---
sam
---
1.0
---
{ a: b }
---
{ a }
---
{}
```

```
paths:
- a[]     # { a: []}
- a{}     # { a: {}}
- []
- {}
- a.b[2]  # THIS in {a: { b: [ 0 0 THIS rest ]}}
- a.'b[]'  # escaped
- a.b{3}.c  # sparse array indexing
- a{4}.c{}    # sparse arrayA { a: { 4: { c: {} }}}
- 
```

```
filesystem mappings
a{}
```
