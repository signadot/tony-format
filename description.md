# codegen: recursive types missing opts parameter

# codegen: recursive types missing opts parameter

## Problem

The code generator fails to pass `opts...` to recursive ToTonyIR/FromTonyIR calls for types defined in the same package.

## Root Cause

The generator uses reflection to check if a type's method accepts opts:

```go
func toTonyIRAcceptsOpts(t reflect.Type) bool {
    method, ok := pt.MethodByName("ToTonyIR")
    if !ok {
        return false  // <-- Returns false for types being generated
    }
    return method.Type.NumIn() > 1
}
```

For recursive types (e.g., `RecursiveSlice` with field `Children []RecursiveSlice`), the `ToTonyIR` method doesn't exist yet at generation time - it's being generated in the same pass. So reflection finds no method, returns false, and `opts...` is not added.

## Failing Tests

```
TestRecursiveSlice
TestRecursiveMap  
TestRecursiveSliceType
TestCrossPackageSlice
```

## Expected Generated Code

```go
node, err := v.ToTonyIR(opts...)
// or
node, err := (&v).ToTonyIR(opts...)
```

## Actual Generated Code

```go
node, err := v.ToTonyIR()
// or  
node, err := (&v).ToTonyIR()
```

## Fix

For types being generated in the current package, assume they will have the full method signature (with opts). The reflection check should only be used for external types.

Introduced in recent changes to handle `*ir.Node` fields which don't have opts parameters.