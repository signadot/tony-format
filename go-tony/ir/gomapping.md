## Overview

YFmt is a restricted set of yaml

## Basic

YFmt is much like json, but extends it in these ways

1. Tags
1. Int-keyed maps aka sparse arrays
1. Textual merge keys in maps (null typed keys)
1. Comments

These extensions permit the format to express diffs betwee
objects in the format and patches for objects in the format
within the format itself.

### Compatibility

Goal: YFmt should read and write json without loss in a way that is 
compatible with Go jsonv2.

We will go through the jsontext package.

## Struct Tags

### YTool Tags

#### Generation

```
struct Inner {
  _ `y:"tag=inner"`
  G int `y:"name=g tag=other"`
}
struct Outer {
  F Inner `y:"name=f tag=funny"`
}
---
# result
!funny.inner
f: 
  g: !other 2
```

#### Consumption

```
struct S {
    Tag: `y:"tag"`
}
```

### Comments

```
struct S {
  Comment []string `y:"comment"`
  LineComment *string `y:"line-comment"`
}
```

## 

## Translation

```
interface {
  ToY(opts ...opt) (*Y, error)
}
interface {
  FromY(*Y, opts ...opt) error
}

struct Inner {
  G int
}
func (in *Inner) ToY(opts ...opt) (*Y, error) {
}
func (in *Inner) FromY(y *Y, opts ...opt) error {
}
struct Outer {
  F Inner
}
func (out *Outer) ToY(opts ...opt) (*Y, error) {
    //...
    fy, err := out.F.ToY(opts...)
    
}
func (out *Outer) FromY(y *Y, opts ...opt) error {
    //...
    f := &Inner{}
    if err := y.From(f, opts...); err != nil {
        return err
    }
    out.F = f
    // ...
}
```



