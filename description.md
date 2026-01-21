# codegen: isZeroValue helper functions use wrong type for named types like time.Duration

## Summary

The generated `isZeroValue_*` functions have incorrect parameter types when the field is a named type (like `time.Duration`). The function signature uses the underlying primitive type (`int64`) instead of the named type (`time.Duration`), causing a compilation error.

## The Issue

- The schema correctly identifies `time.Duration` and uses `!time:duration`
- The generated `isZeroValue_*` functions have signature `func isZeroValue_Skill_Timeout(v int64)`
- But they're called with `s.Timeout` which is `time.Duration`
- Go requires explicit conversion even though `time.Duration` is defined as `type Duration int64`

## Example

The generated code:
```go
if !isZeroValue_Skill_Timeout(s.Timeout) {  // s.Timeout is time.Duration
```

The function signature:
```go
func isZeroValue_Skill_Timeout(v int64) bool {  // expects int64
```

This fails to compile because Go requires explicit conversion between named types and their underlying types.

## Root Cause

In `gomap/codegen/generator.go`, the `getTypeString` function checks `Kind()` before checking if the type has a name:

```go
func getTypeString(t reflect.Type) string {
    // ...
    switch t.Kind() {
    case reflect.Int64:
        return "int64"  // <-- time.Duration hits this case!
    // ...
    default:
        // For named types, use the type name
        if t.Name() != "" {
            return t.Name()  // <-- Never reached for Duration
        }
        return t.String()
    }
}
```

Since `time.Duration` has `Kind() == reflect.Int64`, it returns `"int64"` instead of checking if it's a named type first.

## Proposed Fix

Check if the type is a named type **before** checking the Kind for primitives:

```go
func getTypeString(t reflect.Type) string {
    if t == nil {
        return "interface{}"
    }

    // Check for named types FIRST (before kind switch)
    // This handles types like time.Duration which have an underlying primitive kind
    if t.Name() != "" && t.PkgPath() != "" {
        // It's a named type from a package, use the full qualified name
        return t.String()  // Returns "time.Duration" not "int64"
    }

    switch t.Kind() {
    // ... rest unchanged
    }
}
```