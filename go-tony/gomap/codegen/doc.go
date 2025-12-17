// Package codegen generates Go code for Tony schema types.
//
// Generates ToTony/FromTony methods for Go structs based on Tony schemas,
// enabling efficient serialization without reflection.
//
// Generated code appears in *_gen.go files with ToTonyIR, FromTonyIR,
// ToTony, and FromTony methods.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/gomap - Encoding/decoding
//   - github.com/signadot/tony-format/go-tony/schema - Schema system
package codegen
