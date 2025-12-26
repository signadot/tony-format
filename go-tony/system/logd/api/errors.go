package api

import (
	"fmt"
)

// Error represents an API error response.
//
//tony:schemagen=error,notag
type Error struct {
	Code    string `tony:"name=code"`
	Message string `tony:"name=message"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Message
}

// Is implements the errors.Is interface for error matching.
func (e *Error) Is(target error) bool {
	if e == nil {
		return false
	}
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	// Match by code if target has a code
	if t.Code != "" {
		return e.Code == t.Code
	}
	// If target has no code but has message, match by message
	if t.Message != "" {
		return e.Message == t.Message
	}
	return false
}

// MarshalText implements encoding.TextMarshaler.
// It returns a structured format: "codeLength:code:message" or just "message" if no code.
// The length-prefixed format handles arbitrary message content including colons.
func (e *Error) MarshalText() ([]byte, error) {
	if e == nil {
		return nil, nil
	}
	if e.Code != "" {
		return []byte(fmt.Sprintf("%d:%s:%s", len(e.Code), e.Code, e.Message)), nil
	}
	return []byte(e.Message), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It parses the structured format: "codeLength:code:message" or just "message" if no code.
// The length-prefixed format handles arbitrary message content including colons.
func (e *Error) UnmarshalText(text []byte) error {
	if e == nil {
		return fmt.Errorf("cannot unmarshal into nil Error")
	}
	s := string(text)

	// Try to parse "codeLength:code:message" format
	// Look for first colon, parse length, then read that many chars for code
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			// Found first colon, try to parse length
			var codeLen int
			_, err := fmt.Sscanf(s[:i], "%d", &codeLen)
			if err != nil {
				// Not a number, treat entire string as message
				break
			}
			// Check if we have enough characters for code and another colon
			if i+1+codeLen < len(s) && s[i+1+codeLen] == ':' {
				e.Code = s[i+1 : i+1+codeLen]
				e.Message = s[i+1+codeLen+1:]
				return nil
			}
			// Format doesn't match, treat as message
			break
		}
	}
	// No code found, treat entire string as message
	e.Code = ""
	e.Message = s
	return nil
}

// Common error codes
const (
	ErrCodeInvalidDiff = "invalid_diff"
	ErrCodeInvalidPath = "invalid_path"
	ErrCodeNotFound    = "not_found"
)

// NewError creates a new Error with the given code and message.
func NewError(code, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
	}
}
