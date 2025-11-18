# Design Alternatives for Tony Encoding/Decoding Support

## Goals

1. **JSON Compatibility**: Maintain full compatibility with Go's `encoding/json` package
2. **IR Translation**: Support encoding/decoding via the existing IR (`*y.Y`)
3. **Tag-Triggered Encoders**: Allow custom encoders to be triggered by Tony tags
4. **Flexibility**: Support multiple approaches for different use cases

## Current State

- `y.Y` already implements `MarshalJSON()`/`UnmarshalJSON()` for IR serialization
- `eval.ToJSONAny()`/`FromJSONAny()` convert between IR and JSON-compatible Go types
- Tag system exists with `TagHas()`, `TagGet()`, `HeadTag()`, etc.
- `gomapping.md` proposes `ToY()`/`FromY()` interface pattern

## Design Alternatives

### Option 1: Interface-Based Approach (Go Standard Pattern)

**Pattern**: Similar to `json.Marshaler`/`json.Unmarshaler`

```go
// Core interfaces with generic type parameters
type EncoderTo[T any] interface {
    EncodeToTony() (*y.Y, error)
}

type DecoderFrom[T any] interface {
    DecodeFromTony(*y.Y) (*T, error)
}
```

**EncodeTo Flow**:
1. Check if value implements `EncoderTo[T]` → use it
2. Check if value implements `json.Marshaler` → marshal to JSON, parse to IR
3. Default: convert via IR using reflection

**DecodeFrom Flow**:
1. Check if target implements `DecoderFrom[T]` → use it
2. Check if target implements `json.Unmarshaler` → convert IR to JSON, unmarshal
3. Default: convert IR to Go value using reflection

**Pros**:
- Familiar pattern (matches Go's json package)
- Type-safe
- No global state
- Easy to test

**Cons**:
- Requires modifying types to add encoding
- Can't handle third-party types without wrappers
- Tag-based encoding requires transformers (see Option 1 with transformers)

---

### Option 2: Compositional Tag Transformer Approach

**Pattern**: Tags return transformer functions that compose to modify encoders/decoders

**Key Insight**: An encoder is a generic function `[T any](T) -> (*y.Y, error)`. A tag transformer is a function `EncodeToFunc -> EncodeToFunc` that wraps/modifies the encoder behavior. Tags compose by composing their transformers.

```go
// An encoder function converts a value to Tony IR
type EncodeToFunc[T any] func(T) (*y.Y, error)

// A decoder function converts Tony IR to a value
type DecodeFromFunc[T any] func(*y.Y, *T) (*T, error)

// A tag transformer takes an encoder and returns a modified encoder
// Note: Transformers work with the erased type for composition
type TagEncoderTransformer func(EncodeToFunc[any]) EncodeToFunc[any]

// A tag transformer takes a decoder and returns a modified decoder
type TagDecoderTransformer func(DecodeFromFunc[any]) DecodeFromFunc[any]

// Registry maps head tag to a function that takes args and returns a transformer
type TagEncoderRegistry struct {
    encoderTransformers map[string]func([]string) TagEncoderTransformer  // headTag -> (args -> transformer)
    decoderTransformers map[string]func([]string) TagDecoderTransformer  // headTag -> (args -> transformer)
}

func (r *TagEncoderRegistry) Register(
    headTag string,
    encoderTransformer func([]string) TagEncoderTransformer,
    decoderTransformer func([]string) TagDecoderTransformer,
) {
    r.encoderTransformers[headTag] = encoderTransformer
    r.decoderTransformers[headTag] = decoderTransformer
}
```

**Encoding Flow**:
1. Start with base encoder (from interface, default, or registry lookup)
2. Parse tag chain (e.g., `!outer.inner` -> `[("outer", []), ("inner", [])]`)
3. For each head tag in chain (right-to-left for composition):
   - Lookup transformer factory in registry
   - Call factory with tag args to get transformer
   - Compose transformer with current encoder: `encoder = transformer(encoder)`
4. Apply final composed encoder to value

**Decoding Flow** (reverse):

1. Start with base decoder (from interface, default, or registry lookup)
2. Parse tag chain
3. For each head tag in chain (left-to-right, reverse of encoding):
   - Lookup decoder transformer factory in registry
   - Call factory with tag args to get transformer
   - Compose transformer with current decoder: `decoder = transformer(decoder)`
4. Apply final composed decoder to IR

**Example**:
```go
// Register a tag transformer
registry.Register("outer", 
    // Encoder transformer: wraps encoder to add tag
    func(args []string) TagEncoderTransformer {
        return func(e EncodeToFunc[any]) EncodeToFunc[any] {
            return func(v any) (*y.Y, error) {
                ir, err := e(v)
                if err != nil {
                    return nil, err
                }
                // Add tag to IR
                ir.Tag = TagCompose("!outer", args, ir.Tag)
                return ir, nil
            }
        }
    },
    // Decoder transformer: wraps decoder to handle tag
    func(args []string) TagDecoderTransformer {
        return func(d DecodeFromFunc[any]) DecodeFromFunc[any] {
            return func(ir *y.Y, v *any) (*any, error) {
                // Process tag, then delegate to base decoder
                return d(ir, v)
            }
        }
    },
)

// For tag !outer.inner, transformers compose:
// finalEncoder = outerTransformer(innerTransformer(baseEncoder))
```

**Pros**:
- Elegant compositional design
- Tags naturally compose via function composition
- Works with any type (no interface requirement)
- Flexible: transformers can modify behavior at any level
- Clear separation of concerns

**Cons**:
- More abstract/conceptual
- Requires understanding of function composition
- Slightly more complex implementation

---

### Option 3: Struct Tags for Tony Metadata

**Pattern**: Use struct tags for Tony-specific features (tags, comments, field names)

**Key Insight**: Struct tags serve two purposes:
1. **Tony metadata**: Tags, comments, field names (using `tony:"..."` tag)
2. **Tag composition**: Anonymous fields compose tags via Tony's tag composition rules

```go
// Struct tags for Tony-specific metadata
type Config struct {
    _    `tony:"tag=config"`                    // Anonymous field sets struct-level tag
    Host string `tony:"name=hostname"`         // Field name override
    Port int    `tony:"name=port,omitempty"`   // Field name + omitempty
    Auth Auth   `tony:"tag=auth"`              // Field-level tag
    Comment []string `tony:"comment"`          // Comments
    LineComment *string `tony:"line-comment"`  // Line comment
}

// Tag composition example
type Inner struct {
    _ `tony:"tag=inner"`
    G int `tony:"name=g tag=other"`
}

type Outer struct {
    _ `tony:"tag=outer"`
    F Inner `tony:"name=f"`  // F has tag !inner (from Inner's struct tag, not composed with outer)
}
// When encoding Outer.F, tag is !inner (from Inner type tag, no field tag)
```

**EncodeTo Flow**:
1. Reflect over struct fields
2. Check `tony` struct tag for:
   - `tag=...` - sets tag on field/struct
   - `name=...` - overrides field name
   - `comment` - sets comments
   - `line-comment` - sets line comment
   - `omitempty` - omit if zero value
3. For anonymous fields: compose tags using `TagCompose()`
4. For regular fields with `tag=...`: compose field tag with the field's type tag (if type has one)
5. Apply tags/comments to resulting IR
6. Default: use IR conversion

**Tag Composition Rules**:
- Anonymous field tag composes with parent tag: `!parent.child`
- Field-level tags (`tony:"tag=X"`) compose with the field's type tag (if the type has one)
- Uses `TagCompose()` function for proper tag composition

**Pros**:
- Declarative (tags specify behavior)
- Works with existing structs
- Supports Tony-specific features (tags, comments)
- Respects Tony's tag composition semantics

**Cons**:
- More complex reflection logic
- Need to handle tag composition correctly
- Struct tag parsing overhead

---

### Option 4: Hybrid Approach (Recommended)

**Pattern**: Combine interface-based + registry-based

```go
// Core interfaces (for types that want to implement directly)
type EncoderTo[T any] interface {
    EncodeToTony() (*y.Y, error)
}

type DecoderFrom[T any] interface {
    DecodeFromTony(*y.Y) (*T, error)
}

// Registry for tag-based encoding
type TagEncoderRegistry struct {
    encoderTransformers map[string]func([]string) TagEncoderTransformer
    decoderTransformers map[string]func([]string) TagDecoderTransformer
}

var defaultRegistry = NewTagEncoderRegistry()

func RegisterTagEncoder(headTag string, encoderTransformer func([]string) TagEncoderTransformer, decoderTransformer func([]string) TagDecoderTransformer) {
    defaultRegistry.Register(headTag, encoderTransformer, decoderTransformer)
}
```

**EncodeTo Flow**:
1. **Check interfaces**: If implements `EncoderTo[T]` → use it
2. **Check tag registry**: If IR node has tag → lookup in registry
3. **Check JSON compatibility**: If implements `json.Marshaler` → marshal to JSON, parse to IR
4. **Default**: Convert via IR using reflection

**DecodeFrom Flow**:
1. **Check interfaces**: If implements `DecoderFrom[T]` → use it
2. **Check tag registry**: If IR node has tag → lookup in registry
3. **Check JSON compatibility**: If implements `json.Unmarshaler` → convert IR to JSON, unmarshal
4. **Default**: Convert IR to Go value using reflection

**Pros**:
- Best of both worlds
- Flexible: types can implement interfaces OR use registry
- JSON compatibility maintained
- Tag-based marshaling works for any type

**Cons**:
- More complex implementation
- Two ways to do the same thing (could be confusing)

---

### Option 5: Context-Aware Marshaling

**Pattern**: Pass context/options to marshalers

```go
type EncodeOptions struct {
    Format      format.Format  // Tony, YAML, JSON
    Registry    *TagEncoderRegistry
    PreserveTags bool
    // ... other options
}

type EncoderTo[T any] interface {
    EncodeToTony(opts *EncodeOptions) (*y.Y, error)
}
```

**Pros**:
- Very flexible
- Can customize behavior per-marshal call
- Supports different output formats

**Cons**:
- More complex API
- Options can become unwieldy

---

## Recommended Approach: Option 4 (Hybrid) with Option 5 (Context)

**Combined Design**:

```go
package y

// Core encoding interfaces
type EncoderTo[T any] interface {
    EncodeToTony() (*y.Y, error)
}

type DecoderFrom[T any] interface {
    DecodeFromTony(*y.Y) (*T, error)
}

// Note: Tag-specific interfaces are not needed with compositional transformers.
// Tags compose transformers around the base encoder/decoder from interfaces.

// Registry for tag-based encoding using compositional transformers
// An encoder function converts a value to Tony IR
type EncodeToFunc[T any] func(T) (*y.Y, error)

// A decoder function converts Tony IR to a value
type DecodeFromFunc[T any] func(*y.Y, *T) (*T, error)

// A tag transformer takes an encoder and returns a modified encoder
// Note: Transformers work with the erased type for composition
type TagEncoderTransformer func(EncodeToFunc[any]) EncodeToFunc[any]

// A tag transformer takes a decoder and returns a modified decoder
type TagDecoderTransformer func(DecodeFromFunc[any]) DecodeFromFunc[any]

type TagEncoderRegistry struct {
    encoderTransformers map[string]func([]string) TagEncoderTransformer  // headTag -> (args -> transformer)
    decoderTransformers map[string]func([]string) TagDecoderTransformer  // headTag -> (args -> transformer)
}

func NewTagEncoderRegistry() *TagEncoderRegistry {
    return &TagEncoderRegistry{
        encoderTransformers: make(map[string]func([]string) TagEncoderTransformer),
        decoderTransformers: make(map[string]func([]string) TagDecoderTransformer),
    }
}

// Register registers transformer factories for a head tag
// The factory function receives tag args and returns a transformer
func (r *TagEncoderRegistry) Register(
    headTag string,
    encoderTransformer func([]string) TagEncoderTransformer,
    decoderTransformer func([]string) TagDecoderTransformer,
) {
    r.encoderTransformers[headTag] = encoderTransformer
    r.decoderTransformers[headTag] = decoderTransformer
}

var DefaultTagRegistry = NewTagEncoderRegistry()

// Convenience functions
func RegisterTagEncoder(
    headTag string,
    encoderTransformer func([]string) TagEncoderTransformer,
    decoderTransformer func([]string) TagDecoderTransformer,
) {
    DefaultTagRegistry.Register(headTag, encoderTransformer, decoderTransformer)
}

// Main encoding functions
func EncodeToTony[T any](v T) (*y.Y, error) {
    return EncodeToTonyWithRegistry(v, DefaultTagRegistry)
}

func EncodeToTonyWithRegistry[T any](v T, registry *TagEncoderRegistry) (*y.Y, error) {
    // 1. Get base encoder (from interface, default, or JSON)
    var baseEncoder EncodeToFunc[any]
    
    if e, ok := any(v).(EncoderTo); ok {
        baseEncoder = func(_ any) (*y.Y, error) {
            return e.EncodeToTony()
        }
    } else if m, ok := any(v).(json.Marshaler); ok {
        baseEncoder = func(_ any) (*y.Y, error) {
            jsonBytes, err := m.MarshalJSON()
            if err != nil {
                return nil, err
            }
            return parse.Parse(jsonBytes)
        }
    } else {
        // Default: use reflection-based IR conversion
        baseEncoder = func(val any) (*y.Y, error) {
            return toIR(val, registry)
        }
    }
    
    // 2. Get tag from struct tags (during reflection in toIR)
    // For now, assume tag is determined during struct reflection
    // In practice, toIR() will call composeTagTransformers with the tag
    
    // 3. Compose tag transformers and apply
    // This would be called from toIR() with the appropriate tag
    // For now, return base encoder (transformers applied during struct encoding)
    return baseEncoder(any(v))
}

// Helper to compose tag transformers and apply to encoder
func composeTagTransformers(tag string, registry *TagEncoderRegistry, base EncodeToFunc[any]) EncodeToFunc[any] {
    if tag == "" {
        return base
    }
    
    // Parse tag chain (e.g., "!outer.inner" -> ["outer", "inner"])
    tags := parseTagChain(tag)
    
    // Compose transformers right-to-left (inner to outer)
    result := base
    for i := len(tags) - 1; i >= 0; i-- {
        headTag, args := tags[i].head, tags[i].args
        if factory, ok := registry.encoderTransformers[headTag]; ok {
            transformer := factory(args)
            result = transformer(result)
        }
    }
    
    return result
}

func DecodeFromTony[T any](ir *y.Y, v *T) (*T, error) {
    return DecodeFromTonyWithRegistry(ir, v, DefaultTagRegistry)
}

func DecodeFromTonyWithRegistry[T any](ir *y.Y, v *T, registry *TagEncoderRegistry) (*T, error) {
    // 1. Get base decoder (from interface, default, or JSON)
    var baseDecoder DecodeFromFunc[any]
    
    // Note: Type assertion for generic interface requires type parameter
    // In practice, we'd use reflection or a type switch to handle this
    if d, ok := any(v).(interface{ DecodeFromTony(*y.Y) (any, error) }); ok {
        baseDecoder = func(_ *y.Y, _ *any) (*any, error) {
            result, err := d.DecodeFromTony(ir)
            if err != nil {
                return nil, err
            }
            resultPtr := any(&result)
            return resultPtr.(*any), nil
        }
    } else if u, ok := any(v).(json.Unmarshaler); ok {
        baseDecoder = func(y *y.Y, _ *any) (*any, error) {
            jsonBytes, err := y.MarshalJSON()
            if err != nil {
                return nil, err
            }
            err = u.UnmarshalJSON(jsonBytes)
            if err != nil {
                return nil, err
            }
            return any(v), nil
        }
    } else {
        // Default: use reflection-based IR conversion
        baseDecoder = func(y *y.Y, val *any) (*any, error) {
            err := fromIR(y, val, registry)
            if err != nil {
                return nil, err
            }
            return val, nil
        }
    }
    
    // 2. Compose tag transformers (left-to-right, reverse of encoding)
    finalDecoder := composeDecodeTransformers(ir.Tag, registry, baseDecoder)
    
    // 3. Apply final decoder
    vAny := any(v)
    result, err := finalDecoder(ir, &vAny)
    if err != nil {
        return nil, err
    }
    return result.(*T), nil
}

// Helper to compose tag transformers for decoding (left-to-right)
func composeDecodeTransformers(tag string, registry *TagEncoderRegistry, base DecodeFromFunc[any]) DecodeFromFunc[any] {
    if tag == "" {
        return base
    }
    
    // Parse tag chain
    tags := parseTagChain(tag)
    
    // Compose transformers left-to-right (outer to inner, reverse of encoding)
    result := base
    for i := 0; i < len(tags); i++ {
        headTag, args := tags[i].head, tags[i].args
        if factory, ok := registry.decoderTransformers[headTag]; ok {
            transformer := factory(args)
            result = transformer(result)
        }
    }
    
    return result
}
```

## JSON Compatibility Strategy

**Key Principle**: Tony encoding should be a superset of JSON marshaling.

1. **When encoding to Tony**:
   - If type implements `json.Marshaler`, marshal to JSON first, then parse to IR
   - This ensures JSON-compatible types work with Tony
   - IR can then add tags/comments/etc.

2. **When decoding from Tony**:
   - If type implements `json.Unmarshaler`, convert IR to JSON, then unmarshal
   - This ensures JSON-compatible types can consume Tony data
   - Tags/comments are preserved in IR but ignored during JSON conversion

3. **Round-trip compatibility**:
   - Tony → JSON → Tony should preserve data (tags/comments may be lost)
   - JSON → Tony → JSON should preserve data exactly

## Implementation Phases

### Phase 1: Core Infrastructure
- Add `EncoderTo`/`DecoderFrom` interfaces
- Implement basic `EncodeToTony()`/`DecodeFromTony()` functions
- Add reflection-based IR conversion for basic types

### Phase 2: Tag Registry
- Implement `TagEncoderRegistry`
- Add tag lookup in encoding flow
- Support tag-triggered encoders

### Phase 3: JSON Compatibility
- Integrate `json.Marshaler`/`json.Unmarshaler` fallback
- Ensure round-trip compatibility
- Add tests

### Phase 4: Advanced Features
- Support for struct tags (if desired)
- Context/options support
- Performance optimizations

## Example Usage

```go
// Example 1: Interface-based with tag transformers
type CustomType struct {
    Value string
}

// Base encoding behavior via interface
func (c *CustomType) EncodeToTony() (*y.Y, error) {
    return y.FromString("custom:" + c.Value), nil
}

func (c *CustomType) DecodeFromTony(y *y.Y) (*CustomType, error) {
    c.Value = strings.TrimPrefix(y.String, "custom:")
    return c, nil
}

// Tag transformer can wrap the interface-based encoder
// Register("wrapper", ...) will compose around CustomType.EncodeToTony()
// Usage: CustomType with tag !wrapper
// Flow: wrapperTransformer(baseEncoderFromInterface)(customTypeValue)

// Example 2: Registry-based (handles tag composition)
type DatabaseConfig struct {
    Host string
    Port int
}

func init() {
    // Register by head tag - will match "!dbconfig" or "!outer.dbconfig"
    y.RegisterTagMarshaler("dbconfig", 
        func(v interface{}, fullTag string, args []string) (*y.Y, error) {
            db := v.(*DatabaseConfig)
            return &y.Y{
                Type: y.ObjectType,
                Fields: []*y.Y{y.FromString("host"), y.FromString("port")},
                Values: []*y.Y{y.FromString(db.Host), y.FromInt(int64(db.Port))},
                Tag: fullTag, // Use full composed tag
            }, nil
        },
        func(y *y.Y, v interface{}, fullTag string, args []string) error {
            db := v.(*DatabaseConfig)
            m := y.ToMap()
            db.Host = m["host"].String
            db.Port = int(*m["port"].Int64)
            return nil
        },
    )
}

// Example 3: Struct tags with tag composition
type Inner struct {
    _ `tony:"tag=inner"`
    G int `tony:"name=g tag=other"`
}

type Outer struct {
    _ `tony:"tag=outer"`
    F Inner `tony:"name=f tag=outer"`  // F has tag !outer.inner (field tag composes with Inner's type tag)
    Comment []string `tony:"comment"`
}

// When marshaling Outer:
// - Outer itself has tag !outer
// - F field has tag !outer.inner (field tag "outer" composes with Inner's type tag "inner")
// - Comments are preserved

// Example 4: Anonymous field tag composition
type Base struct {
    _ `tony:"tag=base"`
    ID int `tony:"name=id"`
}

type Extended struct {
    Base  // Anonymous field - tag composes: !extended.base
    _ `tony:"tag=extended"`
    Name string `tony:"name=name"`
}
// Extended marshals as !extended.base with fields from both
// Note: Base's fields inherit the composed tag !extended.base

// Example 5: JSON compatibility
type JSONCompatible struct {
    Data string `json:"data"`
}

// Works automatically via json.Marshaler/Unmarshaler
```

## Struct Tag Specification

### `tony:"..."` Tag Format

The `tony` struct tag supports the following options:

- `tag=NAME` - Sets a tag on the struct/field
  - On anonymous field: composes with parent tag (e.g., `!parent.child`)
  - On regular field: composes with the field's type tag (e.g., if type has `!typeTag`, field tag `!fieldTag` becomes `!fieldTag.typeTag`)
- `name=NAME` - Overrides the field name in Tony output
- `comment` - Field contains comments (type: `[]string`)
- `line-comment` - Field contains line comment (type: `*string`)
- `omitempty` - Omit field if zero value

### Tag Composition Rules

1. **Anonymous fields**: Tag composes with parent using `TagCompose(parentTag, nil, childTag)`
   ```go
   type Inner struct { _ `tony:"tag=inner"` }
   type Outer struct { 
       _ `tony:"tag=outer"`
       Inner  // Results in !outer.inner
   }
   ```

2. **Field-level tags**: Compose with the field's type tag
   ```go
   type HostType struct {
       _ `tony:"tag=hosttype"`
   }
   type Config struct {
       _ `tony:"tag=config"`
       Host HostType `tony:"tag=host"`  // Results in !host.hosttype (field tag composes with type tag)
       Port int `tony:"tag=port"`       // Results in !port (no type tag to compose with)
   }
   ```

3. **Nested composition**: Tags compose left-to-right
   ```go
   // !a.b.c composes as: TagCompose("!a", nil, "!b.c") -> "!a.b.c"
   ```

## Implementation Details

### Tag Registry Matching and Composition

The registry matches transformers by **head tag** (first component):
- Tag `!outer.inner` → matches registry keys `"outer"` and `"inner"`
- Tag `!dbconfig(arg1,arg2)` → matches registry key `"dbconfig"` with args `["arg1", "arg2"]`

**Composition Flow**:
1. Parse tag chain: `!outer.inner` → `[("outer", []), ("inner", [])]`
2. For encoding (right-to-left composition):
   - Start with base encoder
   - Apply `innerTransformer(baseEncoder)` → `encoder1`
   - Apply `outerTransformer(encoder1)` → `finalEncoder`
3. For decoding (left-to-right, reverse):

   - Start with base decoder
   - Apply `outerTransformer(baseDecoder)` → `decoder1`
   - Apply `innerTransformer(decoder1)` → `finalDecoder`

### Struct EncodeTo Algorithm

```go
// Helper to get the tag from a type's struct tags (from anonymous fields)
func getTypeTag(t reflect.Type, registry *TagEncoderRegistry) string {
    if t.Kind() != reflect.Struct {
        return ""
    }
    var typeTag string
    for i := 0; i < t.NumField(); i++ {
        field := t.Field(i)
        if field.Anonymous {
            if tag := parseStructTag(field.Tag.Get("tony")); tag.tag != "" {
                typeTag = TagCompose(typeTag, nil, tag.tag)
            }
        }
    }
    return typeTag
}

func encodeStruct(v reflect.Value, parentTag string, registry *TagEncoderRegistry) (*y.Y, error) {
    t := v.Type()
    result := &y.Y{Type: y.ObjectType}
    
    // 1. Check anonymous fields for struct-level tag
    structTag := parentTag
    for i := 0; i < t.NumField(); i++ {
        field := t.Field(i)
        if field.Anonymous {
            if tag := parseStructTag(field.Tag.Get("tony")); tag.tag != "" {
                structTag = TagCompose(structTag, nil, tag.tag)
            }
        }
    }
    result.Tag = structTag
    
    // 2. Process all fields (including embedded)
    for i := 0; i < t.NumField(); i++ {
        field := t.Field(i)
        fieldTag := parseStructTag(field.Tag.Get("tony"))
        
        // Determine field tag: compose with type tag if present
        var fieldTagValue string
        if field.Anonymous && fieldTag.tag != "" {
            // Anonymous field: compose field tag with struct tag
            fieldTagValue = TagCompose(structTag, nil, fieldTag.tag)
        } else if fieldTag.tag != "" {
            // Regular field: compose field tag with type's tag (if type has one)
            typeTag := getTypeTag(field.Type, registry)  // Get tag from type's struct tags
            if typeTag != "" {
                fieldTagValue = TagCompose(fieldTag.tag, nil, typeTag)
            } else {
                fieldTagValue = fieldTag.tag
            }
        } else {
            // No field tag: inherit struct tag for anonymous fields only, or use type tag for regular fields
            if field.Anonymous {
                fieldTagValue = structTag
            } else {
                // For regular fields without field tag, check if type has a tag
                typeTag := getTypeTag(field.Type, registry)
                if typeTag != "" {
                    fieldTagValue = TagCompose(structTag, nil, typeTag)
                }
            }
        }
        
        // Encode field value
        fieldIR, err := encodeField(v.Field(i), fieldTagValue, registry)
        if err != nil {
            return nil, err
        }
        
        // Apply field name override
        fieldName := field.Name
        if fieldTag.name != "" {
            fieldName = fieldTag.name
        }
        
        result.Fields = append(result.Fields, y.FromString(fieldName))
        result.Values = append(result.Values, fieldIR)
    }
    
    return result, nil
}
```

## Open Questions

1. **Tag argument handling**: Should marshalers receive tag arguments? (Yes - included in design)
2. **Should marshalers be able to modify the tag** during marshaling?
3. **How to handle errors** - should we preserve partial data?
4. **Performance considerations** - should we cache marshaler lookups?
5. **Tag validation**: Should we validate tags match registered marshalers?
6. **Embedded structs**: How to handle multiple anonymous fields with tags?
