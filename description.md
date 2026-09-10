# o m -each

let's add a flag to `o m` which causes it to consider each input document
as an array, and to match items in the array instead of the whole document

if the document is not an array, it should not match.

if there are multiple documents, the output

```
o m -each 'a: b' <<EOF
[]
---
a: b # does not match
---
- a: b
  c: d
- a: b
  c: 1
- a: 2
---
- a: b
  c: 2
EOF
```

should output

```
a: b
c: d
---
a: b
c: 1
---
a: b
c: 2
```