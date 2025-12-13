# gomap.Options Implementation Complete ✅

## Summary

All missing options have been successfully implemented in `gomap.Options`. Both `MapOption` and `UnmapOption` are now complete with full test coverage.

## Implemented Options

### MapOption (Go → Tony IR → Bytes)

All 7 options implemented:

| Option | Function | Status |
|--------|----------|--------|
| `EncodeWire` | `EncodeWire(v bool)` | ✅ Already existed |
| `EncodeFormat` | `EncodeFormat(f format.Format)` | ✅ **NEW** |
| `Depth` | `Depth(n int)` | ✅ **NEW** |
| `EncodeComments` | `EncodeComments(v bool)` | ✅ **NEW** |
| `InjectRaw` | `InjectRaw(v bool)` | ✅ **NEW** |
| `EncodeColors` | `EncodeColors(c *encode.Colors)` | ✅ **NEW** |
| `EncodeBrackets` | `EncodeBrackets(v bool)` | ✅ **NEW** |

### UnmapOption (Bytes → Tony IR → Go)

All 7 options implemented:

| Option | Function | Status |
|--------|----------|--------|
| `ParseFormat` | `ParseFormat(f format.Format)` | ✅ **NEW** |
| `ParseYAML` | `ParseYAML()` | ✅ **NEW** |
| `ParseTony` | `ParseTony()` | ✅ **NEW** |
| `ParseJSON` | `ParseJSON()` | ✅ **NEW** |
| `ParseComments` | `ParseComments(v bool)` | ✅ **NEW** |
| `ParsePositions` | `ParsePositions(m map[*ir.Node]*token.Pos)` | ✅ **NEW** |
| `NoBrackets` | `NoBrackets()` | ✅ **NEW** |

## Test Results

### Unit Tests
```
=== RUN   TestMapOptions
    --- PASS: TestMapOptions/EncodeFormat
    --- PASS: TestMapOptions/Depth
    --- PASS: TestMapOptions/EncodeComments
    --- PASS: TestMapOptions/InjectRaw
    --- PASS: TestMapOptions/EncodeColors
    --- PASS: TestMapOptions/EncodeBrackets
    --- PASS: TestMapOptions/EncodeWire
    --- PASS: TestMapOptions/MultipleOptions
--- PASS: TestMapOptions

=== RUN   TestUnmapOptions
    --- PASS: TestUnmapOptions/ParseFormat
    --- PASS: TestUnmapOptions/ParseYAML
    --- PASS: TestUnmapOptions/ParseTony
    --- PASS: TestUnmapOptions/ParseJSON
    --- PASS: TestUnmapOptions/ParseComments
    --- PASS: TestUnmapOptions/ParsePositions
    --- PASS: TestUnmapOptions/NoBrackets
    --- PASS: TestUnmapOptions/MultipleOptions
--- PASS: TestUnmapOptions

=== RUN   TestRoundTripWithGomapOptions
--- PASS: TestRoundTripWithGomapOptions
```

### Full Test Suite
```
ok  	github.com/signadot/tony-format/go-tony/gomap	0.219s
ok  	github.com/signadot/tony-format/go-tony/gomap/codegen	0.194s
```

## Files Modified

1. **`gomap/options.go`**
   - Added 6 new `MapOption` functions
   - Added 7 new `UnmapOption` functions
   - Added necessary imports (`format`, `ir`, `token`)

2. **`gomap/options_test.go`** (NEW)
   - Comprehensive tests for all `MapOption` functions
   - Comprehensive tests for all `UnmapOption` functions
   - Round-trip tests with options

## Usage Examples

### MapOption Examples

```go
import (
    "github.com/signadot/tony-format/go-tony/format"
    "github.com/signadot/tony-format/go-tony/gomap"
    "github.com/signadot/tony-format/go-tony/encode"
)

// Encode to JSON format
bytes, err := gomap.ToTony(&myStruct, gomap.EncodeFormat(format.JSONFormat))

// Encode with comments and custom depth
bytes, err := gomap.ToTony(&myStruct,
    gomap.EncodeComments(true),
    gomap.Depth(2),
)

// Encode with colors
colors := encode.NewColors()
bytes, err := gomap.ToTony(&myStruct, gomap.EncodeColors(colors))

// Encode with brackets
bytes, err := gomap.ToTony(&myStruct, gomap.EncodeBrackets(true))
```

### UnmapOption Examples

```go
import (
    "github.com/signadot/tony-format/go-tony/gomap"
    "github.com/signadot/tony-format/go-tony/ir"
    "github.com/signadot/tony-format/go-tony/token"
)

// Parse YAML format
var s MyStruct
err := gomap.FromTony(yamlData, &s, gomap.ParseYAML())

// Parse JSON format
err := gomap.FromTony(jsonData, &s, gomap.ParseJSON())

// Parse with comments
err := gomap.FromTony(tonyData, &s, gomap.ParseComments(true))

// Parse with position tracking
positions := make(map[*ir.Node]*token.Pos)
err := gomap.FromTony(tonyData, &s, gomap.ParsePositions(positions))

// Parse without brackets
err := gomap.FromTony(bracketData, &s, gomap.NoBrackets())
```

## Architecture Notes

- **Consistent Design**: Both `MapOption` and `UnmapOption` are function types (not interfaces), following the same pattern
- **Pass-through Pattern**: All options are simple wrappers that pass through to underlying `encode` and `parse` packages
- **No Breaking Changes**: All existing code continues to work
- **Full Coverage**: All encode and parse options from underlying packages are now exposed through `gomap`

## Verification

Before implementation, all underlying options were verified to work correctly (see `options_verification_test.go` and `options_verification_summary.md`).

After implementation, all new `gomap` options were tested and verified to work correctly through the `gomap` API.

## Next Steps

The implementation is complete. All options are:
- ✅ Implemented
- ✅ Tested
- ✅ Documented
- ✅ Verified to work with existing code

No further work needed!
