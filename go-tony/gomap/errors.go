package gomap

import "fmt"

// MarshalError represents an error during marshaling
type MarshalError struct {
	FieldPath string // Field path (e.g., "person.address.street")
	Message   string
	Err       error
}

func (e *MarshalError) Error() string {
	if e.FieldPath != "" {
		return fmt.Sprintf("marshal error at %s: %s", e.FieldPath, e.Message)
	}
	return fmt.Sprintf("marshal error: %s", e.Message)
}

func (e *MarshalError) Unwrap() error {
	return e.Err
}

// UnmarshalError represents an error during unmarshaling
type UnmarshalError struct {
	FieldPath string // Field path (e.g., "person.address.street")
	Message   string
	Err       error
}

func (e *UnmarshalError) Error() string {
	if e.FieldPath != "" {
		return fmt.Sprintf("unmarshal error at %s: %s", e.FieldPath, e.Message)
	}
	return fmt.Sprintf("unmarshal error: %s", e.Message)
}

func (e *UnmarshalError) Unwrap() error {
	return e.Err
}

// SchemaError represents an error during schema resolution
type SchemaError struct {
	SchemaName string
	Message    string
	Err        error
}

func (e *SchemaError) Error() string {
	if e.SchemaName != "" {
		return fmt.Sprintf("schema error for %q: %s", e.SchemaName, e.Message)
	}
	return fmt.Sprintf("schema error: %s", e.Message)
}

func (e *SchemaError) Unwrap() error {
	return e.Err
}

// TypeError represents a type mismatch error
type TypeError struct {
	FieldPath   string
	Expected    string
	Actual      string
	Message     string
}

func (e *TypeError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = fmt.Sprintf("expected %s, got %s", e.Expected, e.Actual)
	}
	if e.FieldPath != "" {
		return fmt.Sprintf("type error at %s: %s", e.FieldPath, msg)
	}
	return fmt.Sprintf("type error: %s", msg)
}
