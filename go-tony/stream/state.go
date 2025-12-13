package stream

import (
	"errors"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/token"
)

// State provides minimal stack/state/path management.
// Just processes tokens and tracks state - no tokenization, no io.Reader.
// Use this if you already have tokens.
//
// Only tracks bracketed structures ({...} and [...]).
// Block-style arrays (TArrayElt) are not tracked.
type State struct {
	// Depth tracking
	depth int // Current bracket nesting depth

	// Path tracking (kinded path syntax: "", "key", "key[0]", "key{0}", "a.b.c")
	currentPath  string            // Current kinded path from root
	pathStack    []string          // Stack of paths for nested structures
	bracketStack []token.TokenType // Stack of bracket types ('[' or '{') for each level

	// Array index tracking
	arrayIndex int // Current array index (incremented in bracketed arrays)

	// Pending state (for key-value pairs)
	pendingKey   string // Key name seen before TColon (TLiteral or TString)
	pendingInt   string // Integer key seen before TColon (for sparse arrays)
	pendingValue bool   // True if we've seen a value token that needs path update
}

// NewState creates a new State for tracking structure state.
func NewState() *State {
	return &State{
		currentPath: "", // Root path is empty string in kinded path syntax
	}
}

// ProcessEvent processes an event and updates state/path tracking.
// Call this for each event in order.
func (s *State) ProcessEvent(event *Event) error {
	// Update depth and path based on event
	return s.updateState(event)
}

// updateState updates depth and path based on event.
func (s *State) updateState(event *Event) error {
	switch event.Type {
	case EventBeginObject:
		// Opening object - push current path and bracket type to stack
		s.pathStack = append(s.pathStack, s.currentPath)
		s.bracketStack = append(s.bracketStack, token.TLCurl)
		s.depth++
		s.pendingValue = false

	case EventEndObject:
		// Closing object - pop path stack and bracket stack
		if s.depth == 0 {
			return errors.New("negative depth")
		}
		s.popBracketStack()
		s.depth--
		s.arrayIndex = 0
		s.pendingKey = ""
		s.pendingInt = ""
		s.pendingValue = false

	case EventBeginArray:
		// Opening array - push current path and bracket type to stack, reset index
		s.pathStack = append(s.pathStack, s.currentPath)
		s.bracketStack = append(s.bracketStack, token.TLSquare)
		s.arrayIndex = 0
		s.depth++
		s.pendingValue = false

	case EventEndArray:
		// Closing array - pop path stack and bracket stack
		if s.depth == 0 {
			return errors.New("negative depth")
		}
		s.popBracketStack()
		s.depth--
		s.arrayIndex = 0
		s.pendingKey = ""
		s.pendingInt = ""
		s.pendingValue = false

	case EventKey:
		// Key event - update path with key
		if len(s.bracketStack) > 0 && s.bracketStack[len(s.bracketStack)-1] == token.TLCurl {
			// Reset to object base before appending key
			s.resetToObjectBase()
		}
		s.appendKeyToPath(event.Key)
		s.pendingValue = true // Next event will be a value

	case EventString, EventInt, EventFloat, EventBool, EventNull:
		// Value events - update path if in array
		if len(s.bracketStack) > 0 && s.bracketStack[len(s.bracketStack)-1] == token.TLSquare {
			// Array element value - append array index to path
			s.appendArrayIndexToPath()
		}
		s.pendingValue = false

	case EventHeadComment, EventLineComment:
		// Comment events - don't affect state (Phase 1: no-op, Phase 2: may affect path)
		// For now, comments don't change state
	}
	return nil
}

// Helper functions for state checks

// resetToObjectBase resets currentPath to the object base from pathStack.
func (s *State) resetToObjectBase() {
	if len(s.pathStack) > 0 {
		s.currentPath = s.pathStack[len(s.pathStack)-1]
	}
}

// popBracketStack pops the path stack and bracket stack.
func (s *State) popBracketStack() {
	if len(s.pathStack) > 0 {
		s.currentPath = s.pathStack[len(s.pathStack)-1]
		s.pathStack = s.pathStack[:len(s.pathStack)-1]
	}
	if len(s.bracketStack) > 0 {
		s.bracketStack = s.bracketStack[:len(s.bracketStack)-1]
	}
}

// appendKeyToPath appends a key to the current path using kinded path syntax.
// Handles special characters by quoting the key.
func (s *State) appendKeyToPath(key string) {
	// Use Quote to properly quote the key if needed
	needsQuote := token.KPathQuoteField(key)
	if s.currentPath == "" {
		// First field - no leading dot
		if needsQuote {
			s.currentPath = token.Quote(key, true)
		} else {
			s.currentPath = key
		}
	} else {
		// Subsequent field - add dot separator
		if needsQuote {
			s.currentPath += "." + token.Quote(key, true)
		} else {
			s.currentPath += "." + key
		}
	}
}

// appendArrayIndexToPath appends the current array index to the path using kinded path syntax.
// If the path already ends with an array index, it resets to the base first.
func (s *State) appendArrayIndexToPath() {
	// Reset to array base if path already ends with an array index
	// This handles cases where we see consecutive array elements without commas
	parent, lastSeg := kpath.RSplit(s.currentPath)
	if strings.HasPrefix(lastSeg, "[") {
		// Path ends with array index - reset to parent
		s.currentPath = parent
	}
	s.currentPath += "[" + strconv.Itoa(s.arrayIndex) + "]"
	s.arrayIndex++
}

// Depth returns the current nesting depth (0 = top level).
func (s *State) Depth() int {
	return s.depth
}

// CurrentPath returns the current kinded path (e.g., "", "key", "key[0]").
func (s *State) CurrentPath() string {
	return s.currentPath
}

// ParentPath returns the parent path (one level up).
func (s *State) ParentPath() string {
	if len(s.pathStack) > 0 {
		return s.pathStack[len(s.pathStack)-1]
	}
	return ""
}

// IsInObject returns true if currently inside an object.
func (s *State) IsInObject() bool {
	return len(s.bracketStack) > 0 && s.bracketStack[len(s.bracketStack)-1] == token.TLCurl
}

// IsInArray returns true if currently inside an array.
func (s *State) IsInArray() bool {
	return len(s.bracketStack) > 0 && s.bracketStack[len(s.bracketStack)-1] == token.TLSquare
}

// CurrentKey returns the current object key (if in object).
func (s *State) CurrentKey() string {
	if !s.IsInObject() {
		return ""
	}
	if s.currentPath == "" {
		return ""
	}

	// Use RSplit to get the last segment, then extract field name
	_, lastSeg := kpath.RSplit(s.currentPath)
	fieldName, ok := kpath.SegmentFieldName(lastSeg)
	if !ok {
		// Not a field segment (could be array index, etc.)
		return ""
	}

	return fieldName
}

// CurrentIndex returns the current array index (if in array).
func (s *State) CurrentIndex() int {
	if s.IsInArray() {
		return s.arrayIndex
	}
	return 0
}

// KPathState creates a State that represents being at the given kinded path.
// It parses the kpath string and directly builds up the State's internal structure
// (path stack, bracket stack, etc.) without processing dummy events.
//
// Returns an error if the kpath string is invalid.
func KPathState(kp string) (*State, error) {
	if kp == "" {
		return NewState(), nil
	}

	// Parse the kpath into structured form
	p, err := kpath.Parse(kp)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return NewState(), nil
	}

	// Build State by directly manipulating state fields for each segment
	state := NewState()
	current := p

	for current != nil {
		if current.Field != nil {
			// Field segment - need to be in an object
			if !state.IsInObject() {
				// Open object: push current path and bracket type
				state.pathStack = append(state.pathStack, state.currentPath)
				state.bracketStack = append(state.bracketStack, token.TLCurl)
				state.depth++
			}
			// Append key to path directly
			state.appendKeyToPath(*current.Field)
			// Move to next segment
			current = current.Next
		} else if current.Index != nil {
			// Dense array index - need to be in an array
			if !state.IsInArray() {
				// Open array: push current path and bracket type
				state.pathStack = append(state.pathStack, state.currentPath)
				state.bracketStack = append(state.bracketStack, token.TLSquare)
				state.depth++
				state.arrayIndex = 0
			}
			// Set array index directly and append to path
			targetIndex := *current.Index
			state.arrayIndex = targetIndex
			// Append array index to path
			parent, lastSeg := kpath.RSplit(state.currentPath)
			if strings.HasPrefix(lastSeg, "[") {
				state.currentPath = parent
			}
			state.currentPath += "[" + strconv.Itoa(targetIndex) + "]"
			state.arrayIndex++ // Increment for next element
			// Move to next segment
			current = current.Next
		} else if current.SparseIndex != nil {
			// Sparse array index - similar to dense but uses {n} syntax
			if !state.IsInArray() {
				state.pathStack = append(state.pathStack, state.currentPath)
				state.bracketStack = append(state.bracketStack, token.TLSquare)
				state.depth++
				state.arrayIndex = 0
			}
			// Set sparse array index directly
			targetIndex := *current.SparseIndex
			state.arrayIndex = targetIndex
			// Append sparse array index to path (using {n} syntax)
			parent, lastSeg := kpath.RSplit(state.currentPath)
			if strings.HasPrefix(lastSeg, "[") || strings.HasPrefix(lastSeg, "{") {
				state.currentPath = parent
			}
			state.currentPath += "{" + strconv.Itoa(targetIndex) + "}"
			state.arrayIndex++ // Increment for next element
			current = current.Next
		} else {
			// Unknown segment type - skip it
			current = current.Next
		}
	}

	return state, nil
}
