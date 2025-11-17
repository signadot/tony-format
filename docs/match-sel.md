#

```tony

!apiop
path: /users
match: !let
let:
- idMatch: "123"
in:
  id: .idMatch
  posts: !apiop
    path: /posts
    authorId: .idMatch
    published: true
    id: null
    title: null
---
path: /users/
match:
  name: bob
---
path: /users
match:
  age: !gt 18
patch:
  adult: true
---
path: /users
!trim
match: !or
- age: !gt 18
- !field.glob "x-*"
  

```
