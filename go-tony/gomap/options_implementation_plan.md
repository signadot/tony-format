# gomap.Options Implementation Plan

## Current State

### MapOption (Go → Tony IR → Bytes)
**Implemented:**
- ✅ `EncodeWire(v bool)` - Controls wire format encoding

**Missing:**
- ❌ `EncodeFormat(f format.Format)` - Control output format (Tony/YAML/JSON)
- ❌ `Depth(n int)` - Control indentation depth
- ❌ `EncodeComments(v bool)` - Include comments in output
- ❌ `InjectRaw(v bool)` - Inject raw values
- ❌ `EncodeColors(c *Colors)` - Color output
- ❌ `EncodeBrackets(v bool)` - Use brackets in output

### UnmapOption (Bytes → Tony IR → Go)
**Implemented:**
- ✅ Function type defined (`UnmapOption func(*unmapConfig)`)
- ✅ `ToParseOptions()` extracts ParseOptions

**Missing:**
- ❌ `ParseYAML()` - Parse as YAML format
- ❌ `ParseTony()` - Parse as Tony format
- ❌ `ParseJSON()` - Parse as JSON format
- ❌ `ParseFormat(f format.Format)` - Parse with specific format
- ❌ `ParseComments(v bool)` - Include comments when parsing
- ❌ `ParsePositions(m map[*ir.Node]*token.Pos)` - Track positions
- ❌ `NoBrackets()` - Disable bracket parsing

## Implementation Plan

### Phase 1: MapOption - Encode Options (Low Complexity)

#### Step 1.1: EncodeFormat
**Goal**: Add `EncodeFormat` MapOption

**Implementation**:
```go
func EncodeFormat(f format.Format) MapOption {
    return func(c *mapConfig) {
        c.EncodeOptions = append(c.EncodeOptions, encode.EncodeFormat(f))
    }
}
```

**Test**:
```go
func TestEncodeFormat(t *testing.T) {
    s := TestStruct{Name: "test", Value: 42}
    
    // Test Tony format (default)
    bytes1, _ := gomap.ToTony(&s, gomap.EncodeFormat(format.TonyFormat))
    // Verify format
    
    // Test YAML format
    bytes2, _ := gomap.ToTony(&s, gomap.EncodeFormat(format.YAMLFormat))
    // Verify YAML format
    
    // Test JSON format
    bytes3, _ := gomap.ToTony(&s, gomap.EncodeFormat(format.JSONFormat))
    // Verify JSON format
}
```

**Verification**:
- [ ] `go test -v -run TestEncodeFormat`
- [ ] Verify each format produces correct output
- [ ] Verify round-trip works for each format

---

#### Step 1.2: Depth
**Goal**: Add `Depth` MapOption

**Implementation**:
```go
func Depth(n int) MapOption {
    return func(c *mapConfig) {
        c.EncodeOptions = append(c.EncodeOptions, encode.Depth(n))
    }
}
```

**Test**:
```go
func TestDepth(t *testing.T) {
    s := TestStruct{Name: "test", Value: 42}
    
    // Test depth 0
    bytes1, _ := gomap.ToTony(&s, gomap.Depth(0))
    // Verify no indentation
    
    // Test depth 2
    bytes2, _ := gomap.ToTony(&s, gomap.Depth(2))
    // Verify 2-space indentation
    
    // Test depth 4
    bytes3, _ := gomap.ToTony(&s, gomap.Depth(4))
    // Verify 4-space indentation
}
```

**Verification**:
- [ ] `go test -v -run TestDepth`
- [ ] Verify indentation matches depth
- [ ] Verify round-trip works with different depths

---

#### Step 1.3: EncodeComments
**Goal**: Add `EncodeComments` MapOption

**Implementation**:
```go
func EncodeComments(v bool) MapOption {
    return func(c *mapConfig) {
        c.EncodeOptions = append(c.EncodeOptions, encode.EncodeComments(v))
    }
}
```

**Test**:
```go
func TestEncodeComments(t *testing.T) {
    // Create IR node with comments
    node := ir.FromMap(map[string]*ir.Node{
        "name": ir.FromString("test"),
    })
    node.Comment = &ir.Node{
        Type:  ir.CommentType,
        Lines: []string{"# Test comment"},
    }
    
    // Test with comments enabled
    bytes1, _ := gomap.ToTony(node, gomap.EncodeComments(true))
    // Verify comments in output
    
    // Test with comments disabled
    bytes2, _ := gomap.ToTony(node, gomap.EncodeComments(false))
    // Verify no comments in output
}
```

**Verification**:
- [ ] `go test -v -run TestEncodeComments`
- [ ] Verify comments appear when enabled
- [ ] Verify comments absent when disabled

---

#### Step 1.4: InjectRaw
**Goal**: Add `InjectRaw` MapOption

**Implementation**:
```go
func InjectRaw(v bool) MapOption {
    return func(c *mapConfig) {
        c.EncodeOptions = append(c.EncodeOptions, encode.InjectRaw(v))
    }
}
```

**Test**:
```go
func TestInjectRaw(t *testing.T) {
    // Test with raw injection enabled/disabled
    // Verify raw values are injected when enabled
}
```

**Verification**:
- [ ] `go test -v -run TestInjectRaw`
- [ ] Verify raw injection behavior

---

#### Step 1.5: EncodeColors
**Goal**: Add `EncodeColors` MapOption

**Implementation**:
```go
func EncodeColors(c *encode.Colors) MapOption {
    return func(cfg *mapConfig) {
        cfg.EncodeOptions = append(cfg.EncodeOptions, encode.EncodeColors(c))
    }
}
```

**Test**:
```go
func TestEncodeColors(t *testing.T) {
    s := TestStruct{Name: "test", Value: 42}
    
    colors := &encode.Colors{...}
    bytes, _ := gomap.ToTony(&s, gomap.EncodeColors(colors))
    // Verify colors in output
}
```

**Verification**:
- [ ] `go test -v -run TestEncodeColors`
- [ ] Verify colors appear in output

---

#### Step 1.6: EncodeBrackets
**Goal**: Add `EncodeBrackets` MapOption

**Implementation**:
```go
func EncodeBrackets(v bool) MapOption {
    return func(c *mapConfig) {
        c.EncodeOptions = append(c.EncodeOptions, encode.EncodeBrackets(v))
    }
}
```

**Test**:
```go
func TestEncodeBrackets(t *testing.T) {
    s := TestStruct{Name: "test", Value: 42}
    
    // Test with brackets enabled
    bytes1, _ := gomap.ToTony(&s, gomap.EncodeBrackets(true))
    // Verify brackets in output
    
    // Test with brackets disabled
    bytes2, _ := gomap.ToTony(&s, gomap.EncodeBrackets(false))
    // Verify no brackets in output
}
```

**Verification**:
- [ ] `go test -v -run TestEncodeBrackets`
- [ ] Verify brackets appear when enabled
- [ ] Verify brackets absent when disabled

---

### Phase 2: UnmapOption - Parse Options (Low Complexity)

#### Step 2.1: ParseFormat
**Goal**: Add `ParseFormat` UnmapOption

**Implementation**:
```go
func ParseFormat(f format.Format) UnmapOption {
    return func(cfg *unmapConfig) {
        cfg.ParseOptions = append(cfg.ParseOptions, parse.ParseFormat(f))
    }
}
```

**Test**:
```go
func TestParseFormat(t *testing.T) {
    // Test parsing Tony format
    yamlData := []byte("name: test\nvalue: 42")
    var s TestStruct
    err := gomap.FromTony(yamlData, &s, gomap.ParseFormat(format.YAMLFormat))
    // Verify parsed correctly
    
    // Test parsing JSON format
    jsonData := []byte(`{"name":"test","value":42}`)
    var s2 TestStruct
    err = gomap.FromTony(jsonData, &s2, gomap.ParseFormat(format.JSONFormat))
    // Verify parsed correctly
}
```

**Verification**:
- [ ] `go test -v -run TestParseFormat`
- [ ] Verify each format parses correctly
- [ ] Verify round-trip works

---

#### Step 2.2: ParseYAML, ParseTony, ParseJSON
**Goal**: Add convenience functions for common formats

**Implementation**:
```go
func ParseYAML() UnmapOption {
    return ParseFormat(format.YAMLFormat)
}

func ParseTony() UnmapOption {
    return ParseFormat(format.TonyFormat)
}

func ParseJSON() UnmapOption {
    return ParseFormat(format.JSONFormat)
}
```

**Test**:
```go
func TestParseYAML(t *testing.T) {
    yamlData := []byte("name: test\nvalue: 42")
    var s TestStruct
    err := gomap.FromTony(yamlData, &s, gomap.ParseYAML())
    // Verify parsed correctly
}

func TestParseTony(t *testing.T) {
    tonyData := []byte("name: test\nvalue: 42")
    var s TestStruct
    err := gomap.FromTony(tonyData, &s, gomap.ParseTony())
    // Verify parsed correctly
}

func TestParseJSON(t *testing.T) {
    jsonData := []byte(`{"name":"test","value":42}`)
    var s TestStruct
    err := gomap.FromTony(jsonData, &s, gomap.ParseJSON())
    // Verify parsed correctly
}
```

**Verification**:
- [ ] `go test -v -run TestParseYAML`
- [ ] `go test -v -run TestParseTony`
- [ ] `go test -v -run TestParseJSON`
- [ ] Verify each convenience function works

---

#### Step 2.3: ParseComments
**Goal**: Add `ParseComments` UnmapOption

**Implementation**:
```go
func ParseComments(v bool) UnmapOption {
    return func(cfg *unmapConfig) {
        cfg.ParseOptions = append(cfg.ParseOptions, parse.ParseComments(v))
    }
}
```

**Test**:
```go
func TestParseComments(t *testing.T) {
    tonyWithComments := []byte("# Comment\nname: test # inline\nvalue: 42")
    
    // Test with comments enabled
    var s1 TestStruct
    err := gomap.FromTony(tonyWithComments, &s1, gomap.ParseComments(true))
    // Verify comments parsed
    
    // Test with comments disabled
    var s2 TestStruct
    err = gomap.FromTony(tonyWithComments, &s2, gomap.ParseComments(false))
    // Verify comments ignored
}
```

**Verification**:
- [ ] `go test -v -run TestParseComments`
- [ ] Verify comments parsed when enabled
- [ ] Verify comments ignored when disabled

---

#### Step 2.4: ParsePositions
**Goal**: Add `ParsePositions` UnmapOption

**Implementation**:
```go
func ParsePositions(m map[*ir.Node]*token.Pos) UnmapOption {
    return func(cfg *unmapConfig) {
        cfg.ParseOptions = append(cfg.ParseOptions, parse.ParsePositions(m))
    }
}
```

**Test**:
```go
func TestParsePositions(t *testing.T) {
    tonyData := []byte("name: test\nvalue: 42")
    positions := make(map[*ir.Node]*token.Pos)
    
    var s TestStruct
    err := gomap.FromTony(tonyData, &s, gomap.ParsePositions(positions))
    // Verify positions populated
    // Verify positions map has entries
}
```

**Verification**:
- [ ] `go test -v -run TestParsePositions`
- [ ] Verify positions map populated
- [ ] Verify positions are correct

---

#### Step 2.5: NoBrackets
**Goal**: Add `NoBrackets` UnmapOption

**Implementation**:
```go
func NoBrackets() UnmapOption {
    return func(cfg *unmapConfig) {
        cfg.ParseOptions = append(cfg.ParseOptions, parse.NoBrackets())
    }
}
```

**Test**:
```go
func TestNoBrackets(t *testing.T) {
    // Test parsing with NoBrackets option
    // Verify bracket parsing disabled
}
```

**Verification**:
- [ ] `go test -v -run TestNoBrackets`
- [ ] Verify bracket parsing disabled

---

### Phase 3: Integration Tests (High Priority)

#### Step 3.1: Combined Options Test
**Goal**: Test multiple options together

**Test**:
```go
func TestCombinedOptions(t *testing.T) {
    s := TestStruct{Name: "test", Value: 42}
    
    // Test multiple MapOptions
    bytes, err := gomap.ToTony(&s,
        gomap.EncodeWire(true),
        gomap.EncodeFormat(format.TonyFormat),
        gomap.EncodeComments(true),
        gomap.Depth(2),
    )
    // Verify all options applied
    
    // Test multiple UnmapOptions
    var s2 TestStruct
    err = gomap.FromTony(bytes, &s2,
        gomap.ParseTony(),
        gomap.ParseComments(true),
    )
    // Verify all options applied
}
```

**Verification**:
- [ ] `go test -v -run TestCombinedOptions`
- [ ] Verify all options work together
- [ ] Verify no conflicts between options

---

#### Step 3.2: Round-Trip Tests
**Goal**: Test round-trip with all options

**Test**:
```go
func TestRoundTripWithOptions(t *testing.T) {
    s := TestStruct{Name: "test", Value: 42}
    
    // Round-trip with various options
    bytes, _ := gomap.ToTony(&s, gomap.EncodeWire(true))
    var s2 TestStruct
    gomap.FromTony(bytes, &s2, gomap.ParseTony())
    // Verify s == s2
    
    // Test with different formats
    bytes2, _ := gomap.ToTony(&s, gomap.EncodeFormat(format.YAMLFormat))
    var s3 TestStruct
    gomap.FromTony(bytes2, &s3, gomap.ParseYAML())
    // Verify s == s3
}
```

**Verification**:
- [ ] `go test -v -run TestRoundTripWithOptions`
- [ ] Verify round-trip works with all option combinations

---

## Summary

### Implementation Order

**Phase 1: MapOption (6 steps)**
1. EncodeFormat
2. Depth
3. EncodeComments
4. InjectRaw
5. EncodeColors
6. EncodeBrackets

**Phase 2: UnmapOption (5 steps)**
1. ParseFormat
2. ParseYAML/ParseTony/ParseJSON (convenience functions)
3. ParseComments
4. ParsePositions
5. NoBrackets

**Phase 3: Integration (2 steps)**
1. Combined options test
2. Round-trip tests

### Estimated Effort

- **Phase 1**: ~2-3 hours (straightforward pass-through functions)
- **Phase 2**: ~1-2 hours (straightforward pass-through functions, same pattern as Phase 1)
- **Phase 3**: ~1-2 hours (comprehensive testing)
- **Total**: ~4-7 hours

### Verification Strategy

Each step includes:
1. **Unit test** - Test the specific option in isolation
2. **Integration test** - Test with real structs
3. **Round-trip test** - Verify option doesn't break round-trip
4. **Edge case test** - Test with empty values, nil, etc.

### Success Criteria

- ✅ All encode options available as MapOptions
- ✅ All parse options available as UnmapOptions
- ✅ All tests pass
- ✅ Round-trip works with all options
- ✅ No breaking changes to existing code
