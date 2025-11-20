# GoMap and Code Generation

GoMap provides bidirectional mapping between Go structs and Tony format, with
automatic code generation for serialization and schema definition.

## Overview

### GoMap and Tony

GoMap enables Go applications to work with Tony format through:

- **Zero-setup reflection** - Use `gomap.{To,From}Tony()` and `gomap.{To,From}TonyIR()` without any configuration
- **Automatic code generation** of `ToTonyIR()` and `FromTony()` methods (optional)
- **Schema definition** from Go struct definitions
- **Type safety** with compile-time checking and runtime validation
- **Cross-package support** with automatic import management

**Two approaches**:
1. **Reflection-based** (zero setup) - Perfect for quick experiments and scripts
2. **Code generation** (optional) - Optimized for production use with schemas

### The `tony-codegen` Tool

`tony-codegen` is a unified code generation tool that:

1. **Generates `.tony` schema files** from structs with `schemadef=` tags
2. **Generates Go code** (`ToTonyIR()`/`FromTony()` methods) for structs with `schema=` or `schemadef=` tags

## Two Ways to Use GoMap

### Option 1: Zero Setup (Reflection-Based)

**Perfect for**: Quick experiments, one-off scripts, debugging

You can use `gomap.ToTonyIR()` and `gomap.FromTonyIR()` **without any schema tags or code generation**:

For even simpler usage, use the byte-based convenience functions `gomap.ToTony()` and `gomap.FromTony()`:

```go
package main

import (
    "github.com/signadot/tony-format/go-tony/gomap"
    "github.com/signadot/tony-format/go-tony/encode"
    "github.com/signadot/tony-format/go-tony/parse"
    "os"
)

type Person struct {
    Name  string
    Age   int
    Email *string  // Optional field (pointer)
}

func main() {
    // No schema tags needed!
    person := Person{Name: "Alice", Age: 30}
    
    // Option 1: Byte-based API (simplest)
    data, err := gomap.ToTony(person)
    if err != nil {
        panic(err)
    }
    
    var p2 Person
    err = gomap.FromTony(data, &p2)
    if err != nil {
        panic(err)
    }
    
    // Option 2: IR-based API (for advanced IR manipulation)
    node, err := gomap.ToTonyIR(person)
    if err != nil {
        panic(err)
    }
    
    // Encode to bytes with options
    var buf bytes.Buffer
    encode.Encode(node, &buf, encode.EncodeComments)
    
    // Or parse from bytes with options
    data = buf.Bytes()
    node2, err := parse.Parse(data, parse.ParseComments)
    
    // Convert back from Tony IR
    var p3 Person
    err = gomap.FromTonyIR(node2, &p3)
    if err != nil {
        panic(err)
    }
}
```

**Benefits**:
- ✅ **Zero setup** - works immediately
- ✅ **No code generation** - no `go generate` step
- ✅ **No schema files** - no `.tony` files needed
- ✅ **Perfect for quick use** - testing, debugging, scripts
- ✅ **Two APIs** - byte-based (`ToTony`/`FromTony`) or IR-based (`ToTonyIR`/`FromTonyIR`)

**Trade-offs**:
- Uses reflection (slightly slower than generated code)
- No compile-time type checking
- Field names match Go struct fields exactly (case-sensitive)

### Option 2: Schema-Driven (Code Generation)

**Perfect for**: Production use, type safety, performance

Use **comment-based directives** (recommended) or struct tags for code generation:

```go
package models

// Person represents a person in the system.
// Use comment directives - cleaner and works with any type!
//tony:schemadef=person
type Person struct {
    // Name is the person's full name
    Name string `tony:"field=name"`
    
    // Age in years
    Age int `tony:"field=age"`
}

// Alternative: struct tag approach (requires dummy field)
type Employee struct {
    _ struct{} `tony:"schemadef=employee"`
    Name string
    Salary int
}
```

**After running `tony-codegen`, use generated methods:**

```go
func main() {
    person := Person{Name: "Alice", Age: 30}
    
    // Primary API: bytes-based methods
    data, err := person.ToTony()      // Serialize to bytes
    
    var p2 Person
    err = p2.FromTony(data)           // Deserialize from bytes
    
    // Advanced: IR-based methods (for IR manipulation)
    node, err := person.ToTonyIR()    // Get IR node
    err = p2.FromTonyIR(node)         // Load from IR node
}
```

**Benefits**:
- ✅ **High performance** - no reflection overhead
- ✅ **Type safety** - compile-time checking
- ✅ **Schema validation** - ensures data matches schema
- ✅ **Custom field names** - map Go fields to schema fields
- ✅ **Works with bytes** - `ToTony()`/`FromTony()` handle bytes directly
- ✅ **Comment-based directives** - cleaner, works with type aliases

**Mixed Reflection/Codegen Pattern**:

For structs that mix generated and non-generated fields, use an embedded struct:

```go
// Generated part - has schema
//tony:schemadef=person-core
type PersonCore struct {
    Name string
    Age  int
}

// Full struct - combines generated + reflection
type Person struct {
    PersonCore              // Generated methods available
    RuntimeData interface{} // Handled by reflection
}

func main() {
    p := Person{
        PersonCore: PersonCore{Name: "Alice", Age: 30},
        RuntimeData: map[string]interface{}{"key": "value"},
    }
    
    // Use generated method for core data
    coreNode, _ := p.PersonCore.ToTonyIR()
    
    // Use reflection for runtime data
    runtimeNode, _ := gomap.ToTonyIR(p.RuntimeData)
    
    // Combine as needed
}
```

**When to use each**:
- **Use reflection** (`gomap.ToTony`/`FromTony` or `ToTonyIR`/`FromTonyIR`) for quick experiments and one-off scripts
- **Use code generation** (generated `ToTony`/`FromTony` methods) for production code and when you need schemas
- **Use comment directives** (`//tony:`) over struct tags - cleaner and more flexible

## Quick Start

### Defining a Struct

```go
package models

// Person represents a person in the system.
//tony:schemadef=person
type Person struct {
    // Name is the person's full name
    Name string `tony:"field=name"`
    
    // Age in years
    Age int `tony:"field=age"`
}
```

### Generating Code

```bash
# Generate schemas and code
tony-codegen

# Or use go generate
go generate ./...
```

This generates:
- `schema_gen.tony` - Schema file (contains all schemas in the package)
- `models_gen.go` - Go code with `ToTony()`, `FromTony()`, `ToTonyIR()`, and `FromTonyIR()` methods

### Using Generated Methods

```go
// Serialize to Tony bytes (primary API)
person := &Person{Name: "Alice", Age: 30}
data, err := person.ToTony()

// Deserialize from Tony bytes
var p Person
err = p.FromTony(data)

// Advanced: Work with IR nodes directly
node, err := person.ToTonyIR()
err = p.FromTonyIR(node)
```
```

## Defining Schemas

There are two ways to define schemas for your structs: using **Doc Comment Directives** (recommended) or **Struct Tags**.

### Doc Comment Directives (Recommended)

Use `//tony:` directives in doc comments. This is the cleanest approach and works for all types.

#### `//tony:schemadef=<name>`

Define a new schema and generate the `.tony` schema file.

```go
// Person represents a person in the system.
//tony:schemadef=person
type Person struct {
    Name string `tony:"field=name"`
    Age  int    `tony:"field=age"`
}
```

#### `//tony:schema=<name>`

Use an existing schema from the filesystem.

```go
//tony:schema=person
type Employee struct {
    Name   string
    Salary int
}
```

**Benefits of Directives:**
- Cleaner code (no dummy fields needed)
- Works on type aliases and interfaces
- Keeps metadata separate from struct definition

### Struct Tags (Alternative)

Alternatively, you can use a dummy field with a struct tag. This is useful if you prefer to keep all metadata within the struct definition.

#### `schemadef=<name>`

```go
type Person struct {
    _ struct{} `tony:"schemadef=person"`
    Name string
}
```

#### `schema=<name>`

```go
type Employee struct {
    _ struct{} `tony:"schema=person"`
    Name   string
    Salary int
}
```

#### Advanced Directives

Multiple directives can be combined. For example, to capture schema comments:

```go
//tony:schemadef=user
//tony:comment=UserComments
type User struct {
    ID   string
    Name string
    UserComments []string // Populated with schema comments during unmarshaling
}
```

Supported directives:
- `//tony:schemadef=<name>` - Define a new schema
- `//tony:schema=<name>` - Use an existing schema
- `//tony:comment=<field>` - Populate field with schema comments

**Note**: If both a struct tag and doc comment directive are present, the struct tag takes precedence.

### Field Tags

#### `field=<name>`

Override the schema field name.

```go
type Person struct {
    FirstName string `tony:"field=first_name"`
}
```

The schema will use `first_name` as the field name instead of `FirstName`.

#### `omit`

Exclude a field from schema and serialization.

```go
type Person struct {
    Password string `tony:"omit"`  // Never serialized
}
```

#### `required`

Mark a field as required. Deserialization fails if the field is missing.

```go
type Person struct {
    ID string `tony:"required"`
}
```

#### `optional`

Mark a field as optional. Zero values are omitted during serialization.

```go
type Person struct {
    Email string `tony:"optional"`
}
```

## Type Mapping

### Basic Types

Go basic types map to Tony IR types:

```go
type Example struct {
    Text   string   // !irtype ""
    Count  int      // !irtype 1
    Score  float64  // !irtype 1
    Active bool     // !irtype true
}
```

### Pointers (Nullable Types)

Pointer types represent optional/nullable values:

```go
type Person struct {
    Email *string  // Optional field
}
```

Schema: `Email: !or [!irtype null, !irtype ""]`

### Slices and Arrays

Slices map to Tony arrays:

```go
type Person struct {
    Tags []string
}
```

Schema: `Tags: !and [.[array], !irtype ""]`

### Maps

Maps with string keys:

```go
type Config struct {
    Metadata map[string]string
}
```

Schema: `Metadata: !irtype {}`

### Nested Structs

Structs with schemas become schema references:

```go
type Person struct {
    schemaTag `tony:"schemadef=person"`
    Name string
}

type Employee struct {
    schemaTag `tony:"schemadef=employee"`
    Person Person  // References person schema
    Salary int
}
```

Generated `employee.tony`:
```tony
define:
  Person: !person  # Schema reference
  Salary: !irtype 1
```

### Cross-Package Types

Types from other packages are automatically handled:

```go
import "github.com/example/format"

type Config struct {
    Format *format.Format  // Named type from another package
}
```

Generated code includes proper imports:
```go
import (
    "github.com/example/format"
    "github.com/signadot/tony-format/go-tony/ir"
)
```

The type resolver:
- Detects cross-package types
- Resolves qualified names (e.g., `format.Format`)
- Adds necessary imports to generated code
- Handles both named types and pointer types

## Schema Generation

### Comment Preservation

Go comments are automatically included in generated schemas:

```go
// Person represents a user.
// Multi-line comments are supported.
type Person struct {
    schemaTag `tony:"schemadef=person"`
    
    // Name is the person's full name
    Name string
}
```

The generated schema includes these comments.

### Grouped Schema Files

When multiple structs in a package have `schemadef=` tags, they are all written to a single `schema_gen.tony` file, separated by `---` document separators:

```tony
# Code generated by tony-codegen. DO NOT EDIT.
context:
- tony-format/context
define:
  Street: !irtype ""
  City: !irtype ""
signature:
  name: address
---
context:
- tony-format/context
define:
  Name: !irtype ""
  Address: !address
signature:
  name: person
```

This approach:
- Reduces file clutter (one schema file per package instead of per struct)
- Makes it easier to manage related schemas
- Maintains clear separation between schemas with `---`
- Allows the schema loader to find schemas by name within the file

### Schema References

Structs can reference other schemas in the same package:

```go
type Address struct {
    schemaTag `tony:"schemadef=address"`
    Street string
    City   string
}

type Person struct {
    schemaTag `tony:"schemadef=person"`
    Name    string
    Address Address  // References address schema
}
```

Generated `schema_gen.tony`:
```tony
define:
  Name: !irtype ""
  Address: !address  # Schema reference
```

### Forward References

Forward references work naturally - structs can reference types defined later:

```go
type Employee struct {
    schemaTag `tony:"schemadef=employee"`
    Name string
    Boss *Person  // Person defined later
}

type Person struct {
    schemaTag `tony:"schemadef=person"`
    Name string
}
```

The code generator:
1. Parses all structs first
2. Builds dependency graph
3. Generates schemas in dependency order
4. Uses schema references for struct types

### Circular Dependencies

Circular dependencies are detected and reported as errors:

```go
type Person struct {
    schemaTag `tony:"schemadef=person"`
    Boss *Employee  // References Employee
}

type Employee struct {
    schemaTag `tony:"schemadef=employee"`
    Manager *Person  // References Person - CIRCULAR!
}
```

Error: `circular dependency detected: person → employee → person`

## Code Generation

### Generated Methods

For each struct with a schema tag, four methods are generated:

#### `ToTony(opts ...encode.EncodeOption) ([]byte, error)` - Primary API

Serializes the struct to Tony format bytes:

```go
func (s *Person) ToTony(opts ...encode.EncodeOption) ([]byte, error) {
    node, err := s.ToTonyIR(opts...)
    if err != nil {
        return nil, err
    }
    var buf bytes.Buffer
    if err := encode.Encode(node, &buf, opts...); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}
```

#### `FromTony(data []byte, opts ...parse.ParseOption) error` - Primary API

Deserializes Tony format bytes into the struct:

```go
func (s *Person) FromTony(data []byte, opts ...parse.ParseOption) error {
    node, err := parse.Parse(data, opts...)
    if err != nil {
        return err
    }
    return s.FromTonyIR(node, opts...)
}
```

#### `ToTonyIR(opts ...encode.EncodeOption) (*ir.Node, error)` - Advanced

Serializes the struct to a Tony IR node (for advanced IR manipulation):

```go
func (s *Person) ToTonyIR(opts ...encode.EncodeOption) (*ir.Node, error) {
    irMap := make(map[string]*ir.Node)
    irMap["name"] = ir.FromString(s.Name)
    irMap["age"] = ir.FromInt(int64(s.Age))
    
    node := ir.FromMap(irMap)
    node.Tag = "!person"
    return node, nil
}
```

#### `FromTonyIR(node *ir.Node, opts ...parse.ParseOption) error` - Advanced

Deserializes a Tony IR node into the struct (for advanced IR manipulation):

```go
func (s *Person) FromTonyIR(node *ir.Node, opts ...parse.ParseOption) error {
    if node.Type != ir.ObjectType {
        return fmt.Errorf("expected object, got %v", node.Type)
    }
    
    if fieldNode := ir.Get(node, "name"); fieldNode != nil {
        if fieldNode.Type != ir.StringType {
            return fmt.Errorf("expected string, got %v", fieldNode.Type)
        }
        s.Name = fieldNode.String
    }
    
    // ... more fields
    return nil
}
```

### Optional Fields

Optional fields (pointers or tagged with `optional`) are handled specially:

```go
type Person struct {
    Email *string `tony:"field=email"`
}
```

Generated `ToTonyIR()`:
```go
// Only serialize if not nil
if s.Email != nil {
    irMap["email"] = ir.FromString(*s.Email)
}
```

Generated `FromTonyIR()`:
```go
// Populate pointer if field exists
if fieldNode := ir.Get(node, "email"); fieldNode != nil {
    val := new(string)
    *val = fieldNode.String
    s.Email = val
}
```

### Required Fields

Required fields are validated during deserialization:

```go
type Person struct {
    ID string `tony:"required"`
}
```

Generated `FromTonyIR()`:
```go
if fieldNode := ir.Get(node, "id"); fieldNode != nil {
    s.ID = fieldNode.String
} else {
    return fmt.Errorf("required field \"id\" is missing")
}
```

### Custom Marshaling

Override generated methods for custom behavior:

```go
// Custom implementation - code generation is skipped
func (p *Person) ToTonyIR() (*ir.Node, error) {
    // Custom serialization logic
    return customNode, nil
}
```

Note: Schema generation still occurs, but code generation is skipped when
methods already exist.

## The `tony-codegen` Tool

### Basic Usage

```bash
# Generate for current package
tony-codegen

# Generate for specific directory
tony-codegen -dir ./models

# Generate recursively
tony-codegen -recursive -dir ./...
```

### Command-Line Flags

- `-o <file>` - Output file for generated Go code (default: `<package>_gen.go`)
- `-dir <path>` - Directory to scan (default: current directory)
- `-recursive` - Scan subdirectories recursively
- `-schema-dir <dir>` - Output directory for schemas, preserves package structure
- `-schema-dir-flat <dir>` - Output directory for schemas, flat structure

### Schema Output Location

#### Default: Package Directory

By default, schemas are written to the same directory as source files:

```
models/
  ├── person.go
  ├── schema_gen.tony    (generated - contains all schemas)
  └── models_gen.go      (generated)
```

#### `-schema-dir` Flag

Preserve package structure in a separate directory:

```bash
tony-codegen -schema-dir ./schemas
```

Result:
```
schemas/
  └── models/
      └── schema_gen.tony
models/
  ├── person.go
  └── models_gen.go
```

#### `-schema-dir-flat` Flag

All schemas in one directory:

```bash
tony-codegen -schema-dir-flat ./schemas
```

Result:
```
schemas/
  └── schema_gen.tony
models/
  ├── person.go
  └── models_gen.go
```

### go generate Integration

```go
//go:generate tony-codegen
package models
```

Or with custom flags:

```go
//go:generate tony-codegen -schema-dir ./schemas
package models
```

## Processing Flow

The code generator follows this sequence:

1. **Discover packages** - Find Go packages to process
2. **Parse structs** - Extract struct definitions and tags
3. **Build dependency graph** - Determine struct dependencies
4. **Detect cycles** - Error if circular dependencies exist
5. **Topological sort** - Order structs by dependencies
6. **Generate schemas** - Create `.tony` files for `schemadef=` structs
7. **Load schemas** - Read schemas for `schema=` structs
8. **Generate code** - Create `ToTony()`, `FromTony()`, `ToTonyIR()`, and `FromTonyIR()` methods

This order ensures:
- Dependencies are processed before dependents
- Forward references work naturally
- Newly generated schemas are available for same-package references

## Error Handling

### Common Errors

#### Schema Not Found

```
Error: schema "person" not found
```

Solution: Ensure the schema file exists or run `tony-codegen` to generate it.

#### Type Mismatch

```
Error: field "age": expected number, got string
```

Solution: Ensure the Tony data matches the struct field types.

#### Required Field Missing

```
Error: required field "id" is missing
```

Solution: Provide all required fields in the Tony data.

#### Circular Dependency

```
Error: circular dependency detected: person → employee → person
```

Solution: Restructure types to eliminate the cycle.

## Examples

### Complete Example

```go
package models

import "time"

type schemaTag struct{}

// User represents a system user
type User struct {
    schemaTag `tony:"schemadef=user"`
    
    // Unique identifier
    ID string `tony:"field=id,required"`
    
    // User's email address
    Email string `tony:"field=email,required"`
    
    // Display name (optional)
    DisplayName *string `tony:"field=display_name"`
    
    // User roles
    Roles []string `tony:"field=roles"`
    
    // Account metadata
    Metadata map[string]string `tony:"field=metadata"`
    
    // Internal field (not serialized)
    password string `tony:"omit"`
}
```

Generated `user.tony`:
```tony
# Code generated by tony-codegen. DO NOT EDIT.
context: tony-format/context
define:
  ID: !irtype ""
  Email: !irtype ""
  DisplayName: !or [!irtype null, !irtype ""]
  Roles: !and [.[array], !irtype ""]
  Metadata: !irtype {}
signature:
  name: user
```

### Usage Example

```go
// Serialize
user := &User{
    ID:    "123",
    Email: "alice@example.com",
    Roles: []string{"admin", "user"},
}
node, err := user.ToTonyIR()
if err != nil {
    log.Fatal(err)
}

// Write to file
file, _ := os.Create("user.tony")
encode.Encode(node, file)

// Read from file
file, _ = os.Open("user.tony")
node, _ = decode.Decode(file)

// Deserialize
var u User
if err := u.FromTonyIR(node); err != nil {
    log.Fatal(err)
}
```

## Implementation Details

### Type Resolver

The type resolver handles cross-package types:

- Detects `*ast.SelectorExpr` (e.g., `format.Format`)
- Uses `golang.org/x/tools/go/packages` for robust type analysis
- Resolves qualified names for named types
- Maintains import mappings in `StructInfo.Imports`

### Code Generator

The generator produces idiomatic Go code:

- Uses `strings.Builder` for efficient string construction
- Formats code with `go/format`
- Adds "DO NOT EDIT" header
- Includes descriptive error messages
- Handles edge cases (overflow checking, type conversions)

### Schema Generator

The schema generator creates valid Tony schemas:

- Uses YAML tag format (`!irtype`, `!and`, `!or`)
- Preserves Go comments in schemas
- Creates schema references for struct types
- Validates schema structure

## Best Practices

1. **Use `schemadef=` for canonical definitions** - Define schemas once
2. **Use `schema=` for reuse** - Reference existing schemas
3. **Mark required fields** - Use `required` tag for mandatory fields
4. **Use pointers for optional fields** - Makes optionality explicit
5. **Add comments** - They appear in generated schemas
6. **Version your schemas** - For backward compatibility
7. **Test serialization** - Round-trip test your structs

## Conclusion

GoMap and `tony-codegen` provide a seamless bridge between Go and Tony format,
enabling type-safe serialization with automatic code generation and schema
validation. The tool handles complex scenarios including cross-package types,
forward references, and nested structures, while maintaining clean, idiomatic
Go code.
