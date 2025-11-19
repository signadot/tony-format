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
					// End of value
					k := strings.TrimSpace(key.String())
					v := value.String()
					// Trim spaces if not quoted (or even if quoted? usually quotes protect spaces)
					// But here we just accumulated.
					// If we want to support `field=name ,`, we need to trim `name `.
					// But if it was `field="name "`, we shouldn't trim.
					// Since we don't track if the *current* value was quoted in a robust way for the whole string
					// (we just toggled inQuote), let's check if we just came out of a quote?
					// Actually, for simplicity and standard tag behavior:
					// If it was quoted, we already consumed the quotes.
					// Standard `reflect.StructTag` doesn't allow spaces around values unless quoted.
					// But we want to be lenient.
					// Let's trim space from value. If user wants spaces, they MUST use quotes.
					v = strings.TrimSpace(v)

					if k != "" {
						result[k] = v
					}
					key.Reset()
					value.Reset()
					inKey = true
					inValue = false
					continue
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
		v := value.String()
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
