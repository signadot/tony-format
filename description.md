# tony-codegen generates wrong signature for isZeroValue with slice of struct types

## Description

When a struct has a field that is a slice of another struct type, the generated `isZeroValue_*` function has the wrong signature.

## Minimal Example

```go
//tony:schemagen=parent
type Parent struct {
    Name     string    `tony:"field=name"`
    Children []Child   `tony:"field=children, optional"`
}

//tony:schemagen=child
type Child struct {
    Name string `tony:"field=name"`
    Age  int    `tony:"field=age"`
}
```

## Expected Generated Code

```go
func isZeroValue_Parent_Children(v []Child) bool {
    return len(v) == 0
}
```

## Actual Generated Code

```go
func isZeroValue_Parent_Children(v Child) bool {
    return len(v) == 0  // compile error: invalid argument for len
}
```

## Error

```
./generated.go:XX: cannot use s.Children (variable of type []Child) as Child value in argument to isZeroValue_Parent_Children
./generated.go:XX: invalid argument: v (variable of struct type Child) for built-in len
```

## Notes

This only happens with slice of struct types (`[]Child`). Slice of primitives (`[]string`) works correctly.