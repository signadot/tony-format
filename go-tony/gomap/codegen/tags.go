package codegen

import (
	"strings"
	"unicode"
)

// ParseStructTag parses a struct tag string (the content inside `...`) into a map.
// It handles key-value pairs (key=value) and boolean flags (key).
// It supports quoted values (key="value with spaces").
// It handles comma-separated values.
func ParseStructTag(tag string) (map[string]string, error) {
	result := make(map[string]string)

	// Simple state machine parser
	var key, value strings.Builder
	inKey := true
	inValue := false
	inQuote := false

	// Trim leading/trailing whitespace
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return result, nil
	}

	runes := []rune(tag)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if inKey {
			if r == '=' {
				inKey = false
				inValue = true
				continue
			} else if r == ',' {
				// End of flag (boolean)
				k := strings.TrimSpace(key.String())
				if k != "" {
					result[k] = ""
				}
				key.Reset()
				continue
			} else if unicode.IsSpace(r) {
				// Ignore spaces in keys, or treat as separator?
				// Standard struct tags don't allow spaces in keys.
				// We'll just accumulate.
			}
			key.WriteRune(r)
		} else if inValue {
			if inQuote {
				if r == '"' {
					// Check for escaped quote? For now, simple quote handling
					// If previous char was backslash, maybe?
					// Go struct tags usually use raw strings, so quotes inside are tricky.
					// We'll assume simple quoting for now.
					inQuote = false
				} else {
					value.WriteRune(r)
				}
			} else {
				if r == '"' && value.Len() == 0 {
					inQuote = true
					continue
				} else if r == ',' {
					// End of value - save this key-value pair and start a new one
					k := strings.TrimSpace(key.String())
					v := strings.TrimSpace(value.String())
					if k != "" {
						result[k] = v
					}
					key.Reset()
					value.Reset()
					inKey = true
					inValue = false
					continue
				} else if unicode.IsSpace(r) {
					// Space in value - could be separator or part of value
					// Check if next non-space character is a potential key start
					peekAhead := i + 1
					for peekAhead < len(runes) && unicode.IsSpace(runes[peekAhead]) {
						peekAhead++
					}
					if peekAhead < len(runes) {
						nextRune := runes[peekAhead]
						// If next non-space char is alphanumeric or underscore (potential key start), treat space as separator
						if (unicode.IsLetter(nextRune) || unicode.IsDigit(nextRune) || nextRune == '_') && !inQuote {
							// Space followed by potential key - save current pair and start new key
							k := strings.TrimSpace(key.String())
							v := strings.TrimSpace(value.String())
							if k != "" {
								result[k] = v
							}
							key.Reset()
							value.Reset()
							inKey = true
							inValue = false
							// Skip all spaces and start reading the new key
							// Set i to peekAhead-1 because the loop will increment it
							i = peekAhead - 1
							continue
						}
					}
					// Space is part of value (or end of string)
					if value.Len() > 0 || inQuote {
						value.WriteRune(r)
					}
					// If value is empty and not quoted, ignore leading space
				} else {
					value.WriteRune(r)
				}
			}
		}
	}

	// Handle last item
	if inKey {
		k := strings.TrimSpace(key.String())
		if k != "" {
			result[k] = ""
		}
	} else if inValue {
		k := strings.TrimSpace(key.String())
		v := strings.TrimSpace(value.String())
		if k != "" {
			result[k] = v
		}
	}

	return result, nil
}

// ParseTonyTag parses the content of a "tony" struct tag.
// Example: `tony:"schemagen=person,optional"` -> tagContent is "schemagen=person,optional"
func ParseTonyTag(tagContent string) (map[string]string, error) {
	return ParseStructTag(tagContent)
}
