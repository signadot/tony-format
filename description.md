# codegen: missing zero-value helper for time.Time optional fields

## Summary

When a struct has an optional `time.Time` field, the generated code references a zero-value helper function that is not generated.

## Reproduction

```go
package example

import "time"

//tony:schemagen=run
type Run struct {
    Started  time.Time `tony:"field=started"`
    Finished time.Time `tony:"field=finished, optional"`
}
```

Run: `tony-codegen -dir .`

## Expected

Generated code should include:
```go
func isZeroValue_Run_Finished(v time.Time) bool {
    return v.IsZero()
}
```

## Actual

The generated code references `isZeroValue_Run_Finished` but the function is not generated:

```go
// Field: Finished (optional)
if !isZeroValue_Run_Finished(s.Finished) {  // <- undefined
    ...
}
```

This causes a compile error: `undefined: isZeroValue_Run_Finished`