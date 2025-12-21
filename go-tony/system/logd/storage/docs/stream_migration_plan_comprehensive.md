# Comprehensive Stream Migration Plan

## Overview

Replace `token.{Sink,Source}` and `parse.NodeParser` with new `stream` package providing:
- `Encoder` - structural encoding with explicit stack management
- `Decoder` - structural event-based decoding
- `State` - minimal core for state management

**Naming**: Types are `stream.Encoder`, `stream.Decoder`, `stream.State` (not `StreamEncoder`/`StreamDecoder`/`StreamState` to avoid stuttering).

**Key Principle**: `stream` package is NEW capability, coexists with `parse`/`encode` (no replacement).

## Migration Strategy

### Phase 1: Create stream Package
- Implement `State` (minimal core)
- Implement `Encoder` (structural encoding)
- Implement `Decoder` (structural event-based decoding)
- Add `NodeToEvents()` / `EventsToNode()` conversion utilities

### Sanity Check: Evaluate Phase 1 Results
- Review API design and implementation
- Test basic functionality
- Evaluate readiness for Phase 2
- **Decision point**: Proceed to Phase 2 or adjust?

### Phase 2: Remove Old Code
- Remove `token.Sink` and `token.Source`
- Remove `parse.NodeParser`
- Clean up any remaining dependencies
- Verify no regressions

## Detailed Step-by-Step Plan

### Phase 1: Create stream Package

#### Step 1.1: Create Package Structure

**Files to create**:
```
go-tony/stream/
├── doc.go              # Package documentation
├── event.go            # Event types and constants
├── state.go            # State (minimal core)
├── encoder.go          # Encoder
├── decoder.go          # Decoder
├── conversion.go        # NodeToEvents, EventsToNode
├── state_test.go
├── encoder_test.go
├── decoder_test.go
└── conversion_test.go
```

**Action**: Create directory structure and empty files.

#### Step 1.2: Define Event Types

**File**: `stream/event.go`

```go
package stream

// Event represents a structural event from the decoder
type Event struct {
    Type EventType
    
    // Value fields (only one is set based on Type)
    Key      string
    String   string
    Int      int64
    Float    float64
    Bool     bool
    
    // Comment fields (for EventHeadComment and EventLineComment)
    CommentLines []string  // Comment text lines (from IR Node.Lines)
}

type EventType int

const (
    EventBeginObject EventType = iota
    EventEndObject
    EventBeginArray
    EventEndArray
    EventKey
    EventString
    EventInt
    EventFloat
    EventBool
    EventNull
    EventHeadComment  // Head comment (precedes a value) - IR: CommentType node with 1 value
    EventLineComment  // Line comment (on same line as value) - IR: CommentType node in Comment field
)

func (t EventType) String() string { ... }
```

**Action**: Implement event types and constants.

**Tests**: Basic event creation and type checking.

#### Step 1.3: Implement State

**File**: `stream/state.go`

**API**:
```go
type State struct {
    // Private: stateMachine, pathStack, nameStack, offset
}

func NewState() *State
func (s *State) ProcessToken(tok token.Token) error
func (s *State) Depth() int
func (s *State) CurrentPath() string
func (s *State) ParentPath() string
func (s *State) IsInObject() bool
func (s *State) IsInArray() bool
func (s *State) CurrentKey() string
func (s *State) CurrentIndex() int
func (s *State) Offset() int64
```

**Implementation Notes**:
- Track depth, path stack, name stack
- Update state on token processing
- Handle bracketed structures only (no block style)
- **Comments**: State doesn't need to track comments (handled at event level)

**Action**: Implement State with tests.

**Tests**:
- State tracking for nested structures
- Path generation
- Key/index tracking

#### Step 1.4: Implement Encoder

**File**: `stream/encoder.go`

**API**:
```go
type Encoder struct {
    // Private: writer, state, offset tracking
}

func NewEncoder(w io.Writer, opts ...StreamOption) (*Encoder, error)

// Queryable State
func (e *Encoder) Depth() int
func (e *Encoder) CurrentPath() string
func (e *Encoder) ParentPath() string
func (e *Encoder) IsInObject() bool
func (e *Encoder) IsInArray() bool
func (e *Encoder) CurrentKey() string
func (e *Encoder) CurrentIndex() int
func (e *Encoder) Offset() int64

// Structure Control
func (e *Encoder) BeginObject() error
func (e *Encoder) EndObject() error
func (e *Encoder) BeginArray() error
func (e *Encoder) EndArray() error

// Value Writing
func (e *Encoder) WriteKey(key string) error
func (e *Encoder) WriteString(value string) error
func (e *Encoder) WriteInt(value int64) error
func (e *Encoder) WriteFloat(value float64) error
func (e *Encoder) WriteBool(value bool) error
func (e *Encoder) WriteNull() error

// Comment Writing (comment-ready API, no-ops in Phase 1)
// Head comment: precedes a value (IR: CommentType node with 1 value in Values)
// Line comment: on same line as value (IR: CommentType node in Comment field)
func (e *Encoder) WriteHeadComment(lines []string) error
func (e *Encoder) WriteLineComment(lines []string) error

// Control
func (e *Encoder) Flush() error
func (e *Encoder) Reset(w io.Writer, opts ...StreamOption)
```

**Comment Methods (Phase 1 Implementation)**:
```go
func (e *Encoder) WriteHeadComment(lines []string) error {
    // No-op for Phase 1
    // Phase 2: Write comment tokens before next value
    return nil
}

func (e *Encoder) WriteLineComment(lines []string) error {
    // No-op for Phase 1
    // Phase 2: Write comment tokens after current value
    return nil
}
```

**StreamOption**:
```go
type StreamOption func(*streamOpts)

type streamOpts struct {
    brackets bool  // Force bracketed style
    wire     bool  // Wire format (implies brackets)
}

func WithBrackets() StreamOption {
    return func(opts *streamOpts) {
        opts.brackets = true
    }
}

func WithWire() StreamOption {
    return func(opts *streamOpts) {
        opts.wire = true
        opts.brackets = true  // Wire format implies brackets
    }
}
```

**Implementation Notes**:
- Use `encode` package logic for formatting (quote strings, format numbers, etc.)
- Track offset on every write
- Update internal State on structure operations
- **Comment methods**: No-ops in Phase 1, will write comments in Phase 2

**Action**: Implement Encoder with tests.

**Tests**:
- Basic encoding (objects, arrays, primitives)
- Nested structures
- Offset tracking
- State queries

#### Step 1.5: Implement Decoder

**File**: `stream/decoder.go`

**API**:
```go
type Decoder struct {
    // Private: reader, tokenization buffer, state
}

func NewDecoder(r io.Reader, opts ...StreamOption) (*Decoder, error)

// ReadEvent reads next structural event
func (d *Decoder) ReadEvent() (Event, error)

// Queryable State (delegates to internal State)
func (d *Decoder) Depth() int
func (d *Decoder) CurrentPath() string
func (d *Decoder) ParentPath() string
func (d *Decoder) IsInObject() bool
func (d *Decoder) IsInArray() bool
func (d *Decoder) CurrentKey() string
func (d *Decoder) CurrentIndex() int
func (d *Decoder) Offset() int64

func (d *Decoder) Reset(r io.Reader, opts ...StreamOption)
```

**Implementation Notes**:
- Use `tokenizeOne()` internally for tokenization
- Convert tokens to events (skip commas, colons)
- **Phase 1**: Skip comment tokens (`TComment`) - don't emit comment events
- **Phase 2**: Parse comment tokens, determine head vs line comment, emit events
- Update internal State on event processing
- **IMPORTANT**: Must validate bracketing requirement

**Bracketing Validation**:
```go
func NewDecoder(r io.Reader, opts ...StreamOption) (*Decoder, error) {
    streamOpts := &streamOpts{}
    for _, opt := range opts {
        opt(streamOpts)
    }
    
    // Validate: must have brackets or wire format
    if !streamOpts.brackets && !streamOpts.wire {
        return nil, fmt.Errorf("stream decoder requires bracketed format (use WithBrackets() or WithWire())")
    }
    
    return &Decoder{
        reader: r,
        state:  NewState(),
        opts:   streamOpts,
        // ... other fields
    }, nil
}
```

**Note**: Same pattern for `NewEncoder` - returns `(*Encoder, error)` and validates bracketing.
```

**Action**: Implement Decoder with tests.

**Tests**:
- Basic decoding (objects, arrays, primitives)
- Event generation (skip commas/colons)
- State tracking
- Bracketing validation
- Error handling (block style tokens)
- Comment handling (Phase 1: skip comments, Phase 2: emit comment events)

**Comment Handling (Phase 1)**:
```go
// Skip comment tokens
if tok.Type == token.TComment {
    continue  // Skip in Phase 1
}
```

**Comment Handling (Phase 2 - Future)**:
```go
// Parse comment tokens
if tok.Type == token.TComment {
    // Determine if head or line comment based on context
    // Emit EventHeadComment or EventLineComment
    return Event{
        Type: EventHeadComment,  // or EventLineComment
        CommentLines: []string{string(tok.Bytes)},
    }, nil
}
```

#### Step 1.6: Implement Conversion Utilities

**File**: `stream/conversion.go`

**API**:
```go
func NodeToEvents(node *ir.Node) ([]Event, error)
func EventsToNode(events []Event) (*ir.Node, error)
func EncodeNode(node *ir.Node, w io.Writer, opts ...StreamOption) error
func DecodeNode(r io.Reader, opts ...StreamOption) (*ir.Node, error)
```

**Action**: Implement conversion functions with tests.

**Comment Handling**:
- **Phase 1**: Skip comments in conversion (don't emit comment events)
- **Phase 2**: Handle head comments (CommentType node with 1 value) and line comments (CommentType in Comment field)

**Tests**:
- Round-trip conversion (Node → Events → Node)
- All node types
- Nested structures
- **Phase 1**: Comments are skipped
- **Phase 2**: Comments are preserved (head and line comments)

#### Step 1.7: Package Documentation

**File**: `stream/doc.go`

```go
// Package stream provides streaming encode/decode for Tony documents.
//
// The stream package provides structural event-based encoding and decoding
// optimized for streaming use cases like snapshot indexing. It only supports
// bracketed structures ({...} and [...]) and does not handle formatting
// options like colors, comments, or block style.
//
// For general parsing/encoding with full feature support, use the parse
// and encode packages instead.
//
// Example:
//
//	// Encoding
//	enc, _ := stream.NewEncoder(writer, stream.WithBrackets())
//	enc.BeginObject()
//	enc.WriteHeadComment([]string{"# Head comment"})  // Comment-ready API
//	enc.WriteKey("name")
//	enc.WriteString("value")
//	enc.WriteLineComment([]string{"# Line comment"})  // Comment-ready API
//	enc.EndObject()
//
//	// Decoding
//	dec, _ := stream.NewDecoder(reader, stream.WithBrackets())
//	event, _ := dec.ReadEvent()  // EventBeginObject
//	// Phase 1: Comments are skipped
//	// Phase 2: event, _ := dec.ReadEvent()  // EventHeadComment
//	event, _ := dec.ReadEvent()  // EventKey("name")
//	event, _ := dec.ReadEvent()  // EventString("value")
//	// Phase 2: event, _ := dec.ReadEvent()  // EventLineComment
//	event, _ := dec.ReadEvent()  // EventEndObject
package stream
```

**Action**: Write comprehensive package documentation.

### Sanity Check: Evaluate Phase 1 Results

#### Step SC.1: Review API Design

**Action**:
1. Review `stream` package API
2. Verify it meets requirements
3. Check for any design issues
4. Get feedback from team

**Checklist**:
- ✅ API is clean and usable
- ✅ Bracketing requirement is enforced
- ✅ Event types are appropriate
- ✅ State queries work correctly
- ✅ Conversion utilities are useful

#### Step SC.2: Test Basic Functionality

**Action**:
1. Test basic encode/decode round-trips
2. Test nested structures
3. Test state queries
4. Test error handling
5. Test conversion utilities

**Test Cases**:
```go
// Round-trip test
node := createTestNode()
events, _ := stream.NodeToEvents(node)
node2, _ := stream.EventsToNode(events)
assert.Equal(node, node2)

// Encode/decode test
enc, _ := stream.NewEncoder(buf, stream.WithBrackets())
// ... encode node
dec, _ := stream.NewDecoder(buf, stream.WithBrackets())
node2, _ := stream.DecodeNode(dec)
assert.Equal(node, node2)

// State query test
dec, _ := stream.NewDecoder(reader, stream.WithBrackets())
event, _ := dec.ReadEvent()  // BeginObject
assert.Equal(1, dec.Depth())
assert.True(dec.IsInObject())
```

#### Step SC.3: Evaluate Readiness

**Action**:
1. Review test coverage
2. Check for any issues or concerns
3. Evaluate if ready for Phase 2
4. **Decision**: Proceed to Phase 2 or adjust?

**Criteria**:
- ✅ All tests passing
- ✅ API is stable
- ✅ No major issues found
- ✅ Ready to remove old code

**If Issues Found**:
- Fix issues in Phase 1
- Re-run sanity check
- Don't proceed to Phase 2 until ready

### Phase 2: Remove Old Code

#### Step 2.1: Verify No Dependencies

**Action**:
1. Search codebase for `token.Sink` usage
2. Search codebase for `token.Source` usage
3. Search codebase for `parse.NodeParser` usage
4. Verify no remaining users

**Command**:
```bash
grep -r "token\.Sink" .
grep -r "token\.Source" .
grep -r "NodeParser" .
```

**Expected**: No results (or only in tests/docs)

#### Step 2.2: Remove token.Sink

**Files to remove**:
- `token/sink.go`
- `token/sink_test.go`

**Action**: 
1. Delete files
2. Run tests to verify no regressions
3. Check for any broken imports

#### Step 2.3: Remove token.Source

**Files to remove**:
- `token/source.go`
- `token/source_test.go`

**Action**: 
1. Delete files
2. Run tests to verify no regressions
3. Check for any broken imports

#### Step 2.4: Remove parse.NodeParser

**Files to remove**:
- `parse/incremental.go` (if it only contains NodeParser)
- `parse/incremental_test.go`

**Action**: 
1. Delete files
2. Run tests to verify no regressions
3. Check for any broken imports

#### Step 2.5: Clean Up Dependencies

**Action**:
1. Remove any unused imports
2. Remove any helper functions only used by removed code
3. Update package documentation if needed
4. Run `go mod tidy` if needed

#### Step 2.6: Final Verification

**Action**:
1. Run full test suite
2. Verify no regressions
3. Check code coverage
4. Update migration documentation
5. Verify codebase is cleaner

## API Fix: Bracketing Requirement

### Issue

`NewStreamDecoder` should require bracketing to be explicitly specified.

### Current API (Problem)

```go
func NewStreamDecoder(r io.Reader, opts ...StreamOption) *StreamDecoder
// Returns decoder even if bracketing not specified
```

### Fixed API

```go
func NewStreamDecoder(r io.Reader, opts ...StreamOption) (*StreamDecoder, error)
// Returns error if bracketing not specified
```

### Implementation

```go
func NewStreamDecoder(r io.Reader, opts ...StreamOption) (*StreamDecoder, error) {
    streamOpts := &streamOpts{}
    for _, opt := range opts {
        opt(streamOpts)
    }
    
    // Validate: must have brackets or wire format
    if !streamOpts.brackets && !streamOpts.wire {
        return nil, fmt.Errorf("stream decoder requires bracketed format: use stream.WithBrackets() or stream.WithWire()")
    }
    
    return &StreamDecoder{
        reader: r,
        state:  NewStreamState(),
        opts:   streamOpts,
        // ... initialize other fields
    }, nil
}
```

**Note**: `NewEncoder` follows the same pattern - returns `(*Encoder, error)` and validates bracketing requirement.

### Updated Usage

```go
// Before (would silently fail - but now returns error)
dec, err := stream.NewDecoder(reader)  // Error: bracketing required

// After (explicit requirement)
dec, err := stream.NewDecoder(reader, stream.WithBrackets())
if err != nil {
    return err
}

// Or for wire format
dec, err := stream.NewDecoder(reader, stream.WithWire())
if err != nil {
    return err
}
```

## Timeline Estimate

### Phase 1: Create stream Package
- **Step 1.1-1.2**: 1 day (package structure, events)
- **Step 1.3**: 2-3 days (State)
- **Step 1.4**: 3-4 days (Encoder)
- **Step 1.5**: 3-4 days (Decoder)
- **Step 1.6**: 2 days (conversion utilities)
- **Step 1.7**: 1 day (documentation)

**Total Phase 1**: ~12-15 days

### Sanity Check: Evaluate Phase 1 Results
- **Step SC.1**: 1 day (review API design)
- **Step SC.2**: 1 day (test basic functionality)
- **Step SC.3**: 0.5 day (evaluate readiness)

**Total Sanity Check**: ~2-3 days

### Phase 2: Remove Old Code
- **Step 2.1**: 0.5 day (verify dependencies)
- **Step 2.2-2.4**: 1 day (remove files)
- **Step 2.5-2.6**: 1 day (cleanup, verification)

**Total Phase 2**: ~2-3 days

**Total Estimate**: ~16-21 days (~3-4 weeks)

## Risk Mitigation

### Risk 1: Breaking Existing Code
**Mitigation**: 
- Keep `parse`/`encode` unchanged
- Only remove code after verification
- Comprehensive testing

### Risk 2: Missing Features
**Mitigation**:
- Clear scope: bracketed structures only
- Document limitations
- Can add features later if needed

### Risk 3: Performance Regression
**Mitigation**:
- Benchmark before/after
- Profile if needed
- Optimize hot paths

### Risk 4: API Design Issues
**Mitigation**:
- Review API design thoroughly
- Get feedback early
- Can adjust before Phase 2

## Success Criteria

### Phase 1 Complete
- ✅ `stream` package implemented
- ✅ All tests passing
- ✅ Documentation complete
- ✅ API reviewed and approved
- ✅ Bracketing requirement enforced

### Sanity Check Passed
- ✅ API design validated
- ✅ Basic functionality verified
- ✅ No major issues found
- ✅ Ready for Phase 2

### Phase 2 Complete
- ✅ `token.Sink` removed
- ✅ `token.Source` removed
- ✅ `parse.NodeParser` removed
- ✅ No regressions
- ✅ Codebase cleaner

## Rollback Plan

If issues arise:
1. **Phase 1**: Can abandon before sanity check
2. **Sanity Check**: Can fix issues and re-run, or abandon if major problems
3. **Phase 2**: Can restore removed files from git if needed

## Next Steps

1. Review and approve this plan
2. Start Phase 1, Step 1.1
3. Regular checkpoints after each step
4. Run sanity check after Phase 1
5. **Decision point**: Proceed to Phase 2 or adjust?
6. Adjust plan based on learnings
