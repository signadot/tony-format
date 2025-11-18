package gomap

// Package gomap provides schema-driven conversion between Go structs and Tony IR nodes.
//
// It supports both reflection-based (zero setup) and code-generated (optimized) conversion:
//   - Reflection-based (default): Works immediately, zero setup, perfect for one-off use
//   - Code-generated (optional): High performance, type-safe, for production use
//   - Smart defaults: Automatically uses generated code when available, falls back to reflection
//
// Field visibility:
//   - Only exported (uppercase) struct fields are processed (like encoding/json)
//   - Unexported fields are ignored (cannot access from different package)
//   - Anonymous fields with schema tags can be unexported (tags are readable)
//   - Field matching is case-sensitive (like encoding/json/v2)
//
// Example usage:
//
//   // One-off use case (zero setup):
//   person := Person{Name: "Alice", Age: 30}
//   node, err := gomap.ToIR(person)  // Uses reflection
//
//   // After code generation (optional optimization):
//   node, err := person.ToTony()  // Uses generated code
//
//   // Unmarshaling:
//   var person2 Person
//   err = gomap.FromIR(node, &person2)  // Uses reflection or generated code
