package stream

import (
	"errors"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// State provides minimal stack/state/path management.
// Just processes tokens and tracks state - no tokenization, no io.Reader.
// Use this if you already have tokens.
//
// Only tracks bracketed structures ({...} and [...]).
// Block-style arrays (TArrayElt) are not tracked.
type State struct {
	stack []item
}

type item struct {
	segment *kpath.KPath
	kind    *kpath.EntryKind
	n       int
	hasKey  bool
}

func (i *item) inc() {
	i.n++
	if i.segment == nil {
		if i.kind == &arr {
			i.segment = kpath.Index(i.n)
		}
		return
	}
	if i.segment.Index == nil {
		return
	}
	*i.segment.Index++
}

var (
	obj = kpath.FieldEntry
	arr = kpath.ArrayEntry
	spr = kpath.SparseArrayEntry
)

// NewState creates a new State for tracking structure state.
func NewState() *State {
	return &State{}
}

func (s *State) pop() {
	n := len(s.stack)
	s.stack = s.stack[:n-1]
}

func (s *State) current() *item {
	n := len(s.stack)
	return &s.stack[n-1]
}

// ProcessEvent processes an event and updates state/path tracking.
// Call this for each event in order.
func (s *State) ProcessEvent(event *Event) error {
	switch event.Type {
	case EventBeginObject:
		if s.Depth() > 0 {
			cur := s.current()
			cur.inc()
			cur.hasKey = false
		}
		s.stack = append(s.stack, item{kind: &obj})

	case EventEndObject:
		if s.Depth() <= 0 {
			return errors.New("negative depth")
		}
		cur := s.current()
		if cur.hasKey {
			return errors.New("key or int key, no val")
		}
		s.pop()

	case EventBeginArray:
		if s.Depth() > 0 {
			cur := s.current()
			cur.inc()
			cur.hasKey = false
		}
		s.stack = append(s.stack, item{kind: &arr, n: -1})

	case EventEndArray:
		if s.Depth() <= 0 {
			return errors.New("negative depth")
		}
		s.pop()
	case EventString, EventInt, EventFloat, EventBool, EventNull:
		if s.Depth() > 0 {
			cur := s.current()
			cur.inc()
			cur.hasKey = false
		}
	case EventKey:
		if len(s.stack) == 0 {
			return errors.New("key not in obj")
		}
		cur := s.current()
		if cur.kind != &obj {
			return errors.New("key not in obj (2): " + s.CurrentPath())
		}
		if cur.hasKey {
			return errors.New("key after key")
		}
		cur.hasKey = true
		cur.segment = kpath.Field(event.Key)
	case EventIntKey:
		if len(s.stack) == 0 {
			return errors.New("int key not in sparse array")
		}
		cur := s.current()
		if cur.hasKey {
			return errors.New("int key after int key")
		}
		if cur.n == 0 && cur.kind == &obj {
			cur.kind = &spr
		}
		if cur.kind != &spr {
			return errors.New("int key not in sparse array")
		}
		cur.hasKey = true
		cur.segment = kpath.SparseIndex(int(event.IntKey))

	case EventHeadComment, EventLineComment:
		// Comment events - don't affect state (Phase 1: no-op, Phase 2: may affect path)
		// For now, comments don't change state
	}
	return nil
}

// Depth returns the current nesting depth (0 = top level).
func (s *State) Depth() int {
	return len(s.stack)
}

// CurrentPath returns the current kinded path (e.g., "", "key", "key[0]").
//
// Skips nil segments (arrays before first element processed). nil segments
// only occur at the top of the stack during normal processing, so this has
// no effect. Skipping allows manually-constructed stacks to include nil
// segments in the middle without breaking path construction.
func (s *State) CurrentPath() string {
	res := ""
	for i := range s.stack {
		item := &s.stack[i]
		if item.segment == nil {
			continue
		}
		if item.segment.Field != nil && i > 0 {
			res += "."
		}
		res += item.segment.String()
	}
	return res
}

// IsInObject returns true if currently inside an object.
func (s *State) IsInObject() bool {
	if len(s.stack) == 0 {
		return false
	}
	cur := s.current()
	if cur.kind != nil {
		return cur.kind == &obj
	}
	if cur.segment != nil {
		return cur.segment.Field != nil
	}
	panic("impossible")
}

// IsInArray returns true if currently inside an array.
func (s *State) IsInArray() bool {
	if len(s.stack) == 0 {
		return false
	}
	cur := s.current()
	if cur.kind != nil {
		return cur.kind == &arr
	}
	if cur.segment != nil {
		return cur.segment.Index != nil
	}
	panic("impossible")
}

func (s *State) IsInSparseArray() bool {
	if len(s.stack) == 0 {
		return false
	}
	cur := s.current()
	if cur.kind != nil {
		return cur.kind == &spr
	}
	if cur.segment != nil {
		return cur.segment.SparseIndex != nil
	}
	panic("impossible")
}

// CurrentKey returns the current object key (if in object).
func (s *State) CurrentKey() (string, bool) {
	if len(s.stack) == 0 {
		return "", false
	}
	cur := s.current()
	if cur.segment == nil {
		return "", false
	}
	if cur.segment.Field == nil {
		return "", false
	}
	return *cur.segment.Field, true
}

func (s *State) CurrentIntKey() (int, bool) {
	if len(s.stack) == 0 {
		return 0, false
	}
	cur := s.current()
	if cur.segment == nil {
		return 0, false
	}
	if cur.segment.SparseIndex == nil {
		return 0, false
	}
	return *cur.segment.SparseIndex, true
}

// CurrentIndex returns the current array index (if in array), -1 otherwise
func (s *State) CurrentIndex() (int, bool) {
	if len(s.stack) == 0 {
		return 0, false
	}
	cur := s.current()
	if cur.segment == nil {
		return 0, false
	}
	if cur.segment.Index == nil {
		return 0, false
	}
	return *cur.segment.Index, true
}
