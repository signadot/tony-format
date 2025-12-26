# logd: race in index tree serialization

GobEncode iterates the tree while updateBounds modifies it.

```
Read at node.go:282 (parentRange during GobEncode)
Write at node.go (updateBounds during indexing)
```

The index needs read locking during persistence, or GobEncode
needs to clone/snapshot the tree.

Discovered while fixing #96.