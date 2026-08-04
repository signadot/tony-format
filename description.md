# mergeop: a !key(name) patch element that omits the key panics with a nil deref instead of erroring

A keyed-list patch whose element does not carry the merge key crashes the process.

`build.tony`:

```
build:
  env: {policy: Never}
  sources:
  - dir: src
  patches:
  - match:
      kind: Pod
    patch:
      spec:
        containers: !key(name)
        - imagePullPolicy: $[policy]
```

`src/pod.yaml`: any Pod with one container.

```
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x2 addr=0x58 pc=0x100d5c628]

goroutine 1 [running]:
github.com/signadot/tony-format/go-tony/mergeop.yKeyOf(...)
	go-tony/mergeop/keyed_list.go:158 +0xc8
	go-tony/mergeop/keyed_list.go:51
	go-tony/patch.go:72
	...
	go-tony/dirbuild/patch.go:33
```

`yKeyOf` builds the path `$.name`, calls `y.GetPath(p)`, and uses `v` without checking
either the error or a nil node — line 158 is `orgTag := v.Tag` on the value GetPath did not
find. Adding the key (`- name: c`) makes the same build render correctly, so the merge
itself is fine; it is only the missing-key case that has no handling.

The right answer is presumably an error naming the patch element and the key it lacks,
since a keyed-list element without its key is not merely unmatched — it is unmergeable, and
silently appending it would be worse than refusing.

Worth checking while there: whether the same path can be reached with the key PRESENT but
null, and whether `GetPath`'s error return is dropped anywhere else in mergeop.

Checked at a9b63d9 and at go-tony v0.0.113.

Context: found writing verse's `deploy/build.tony`. The patch was meant to set
`imagePullPolicy` on three differently-named containers at once, so omitting the name was a
deliberate (and wrong) attempt at "the only container" — an easy mistake to make, and one
that took a stack trace to diagnose rather than a message.