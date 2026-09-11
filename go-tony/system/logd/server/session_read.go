package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/ident"
)

// Reading: a match, and the documents a read and a watch are answered from.
//
// A read runs OFF the request loop (see dispatch), so what is here may be running
// while later requests are handled.

// handleMatch handles match (read) requests.
func (s *Session) handleMatch(id *string, req *api.MatchRequest) {
	path := req.Path

	// Validate path
	if err := validateDataPath(path); err != nil {
		s.sendError(id, api.ErrCodeInvalidPath, err.Error())
		return
	}
	// And spelled as the store spells it: an element of a keyed array is addressed by its
	// name, whichever sugar the client used (ident.CanonicalPath).
	if canon, err := ident.CanonicalPath(s.storage.SchemaFor(s.scopeID()), path); err != nil {
		s.sendError(id, api.ErrCodeInvalidPath, err.Error())
		return
	} else {
		path = canon
	}

	// Resolve the commit to read at: an explicit historical commit if the request
	// carries one, otherwise the current commit. A historical commit must fall in
	// [0, current]; a commit past current would silently read as current and a
	// negative one as empty, so reject both rather than return misleading state.
	current, err := s.storage.GetCurrentCommit()
	if err != nil {
		s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to get current commit: %v", err))
		return
	}
	commit := current
	if req.Commit != nil {
		commit = *req.Commit
		if commit < 0 || commit > current {
			s.sendError(id, api.ErrCodeCommitNotFound,
				fmt.Sprintf("commit %d out of range [0, %d]", commit, current))
			return
		}
	}

	// A match with no pattern, in a view whose schema declares no keyed array, is
	// answered from the store's event stream: the body is encoded as it is read, into
	// the frame that goes out, and no node of it is built (rebuild_plan.md decision 3).
	// What is held is the encoded frame, under the session's budget, and a body past
	// the budget is refused as a node past it was. A pattern needs the node to filter,
	// and a keyed array needs it to be raised into the client's vocabulary; those reads
	// build the node under the same budget.
	if (req.Data == nil || req.Data.Type == ir.NullType) && !s.raises() {
		if err := s.encodedMatch(id, path, commit); err != nil {
			s.sendReadError(id, err)
		}
		return
	}

	// The value at the path, in the session's view. A bad segment is the client's path
	// being wrong, not its data being missing: reporting it as not-found reads as
	// "nothing there yet" and invites a retry that can never succeed.
	state, err := s.readValueAt(path, commit)
	if err != nil {
		s.sendReadError(id, err)
		return
	}

	// Apply match filter if provided
	if req.Data != nil && req.Data.Type != ir.NullType {
		filteredState, err := filterState(state, req.Data)
		if err != nil {
			s.sendError(id, api.ErrCodeMatch, fmt.Sprintf("failed to apply match filter: %v", err))
			return
		}
		state = filteredState
	}

	s.send(api.NewMatchResponse(id, commit, state))
}

// sendReadError answers a read that could not be answered, by what kept it from being.
func (s *Session) sendReadError(id *string, err error) {
	var pe *PathError
	switch {
	case errors.As(err, &pe) && pe.Kind == PathBadSegment:
		s.sendError(id, api.ErrCodeInvalidPath, err.Error())
	case errors.As(err, &pe) && pe.Kind == PathTypeConflict:
		// Something IS there, in a shape that cannot hold what was asked for. Saying
		// not_found here tells a client to wait for a value which has already
		// arrived and is the wrong kind.
		s.sendError(id, api.ErrCodePathConflict, err.Error())
	case errors.Is(err, ErrPathNotFound):
		s.sendError(id, api.ErrCodeNotFound, err.Error())
	default:
		s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to read state: %v", err))
	}
}

// raises says whether the session's view has keyed arrays to raise into the client's
// vocabulary (storage.RaiseState), which a streamed body cannot do.
func (s *Session) raises() bool {
	schema := s.storage.SchemaFor(s.scopeID())
	return schema != nil && len(schema.KeyedPaths()) > 0
}

// encodedMatch answers a match from the store's event stream, encoding the response --
// {id, result: {match: {commit, body}}} -- in the wire form the node encoder writes, with
// the body's events taken from the cursor one at a time, into the frame the writer sends.
//
// It runs OFF the request loop, as every read does, and the frame goes out whole: a body
// encoded on the writer as it was read would hold the connection for the fold, and every
// response behind it -- a write's, in particular -- would wait for a read it has nothing
// to do with, which is the ordering 7qayp3hah12kscx2gdn0 bought. So the fold's cost is
// paid here, in bytes rather than in a node, and the writer's is the write.
//
// An absent path is answered as a read of it is, before anything is encoded.
func (s *Session) encodedMatch(id *string, path string, commit int64) error {
	if commit == 0 {
		return s.classifyAbsent(path, commit)
	}
	c, err := s.storage.Read(commit, s.scopeID(), path)
	if err != nil {
		return err
	}
	defer c.Close()
	if c.Presence() == storage.Absent {
		return s.classifyAbsent(path, commit)
	}
	frame := &budgetedFrame{budget: s.readBudget}
	enc, err := stream.NewEncoder(frame, stream.WithWire())
	if err != nil {
		return err
	}
	if err := enc.BeginObject(); err != nil {
		return err
	}
	if id != nil {
		if err := enc.WriteKey("id"); err != nil {
			return err
		}
		if err := enc.WriteString(*id); err != nil {
			return err
		}
	}
	for _, key := range []string{"result", "match"} {
		if err := enc.WriteKey(key); err != nil {
			return err
		}
		if err := enc.BeginObject(); err != nil {
			return err
		}
	}
	if err := enc.WriteKey("commit"); err != nil {
		return err
	}
	if err := enc.WriteInt(commit); err != nil {
		return err
	}
	if err := enc.WriteKey("body"); err != nil {
		return err
	}
	for {
		ev, err := c.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err := writeEvent(enc, ev); err != nil {
			return err
		}
	}
	for range 3 {
		if err := enc.EndObject(); err != nil {
			return err
		}
	}
	frame.buf.WriteByte('\n')
	s.sendFrame(frame.buf.Bytes())
	return nil
}

// budgetedFrame collects an encoded response and refuses to grow past the budget.
type budgetedFrame struct {
	buf    bytes.Buffer
	budget int64
}

func (f *budgetedFrame) Write(p []byte) (int, error) {
	if int64(f.buf.Len()+len(p)) > f.budget {
		return 0, fmt.Errorf("%w of %d bytes", storage.ErrBudget, f.budget)
	}
	return f.buf.Write(p)
}

// writeEvent hands one of the store's events to the encoder.
func writeEvent(enc *stream.Encoder, ev *stream.Event) error {
	if ev.Tag != "" && ev.IsValueStart() {
		if err := enc.Tag(ev.Tag); err != nil {
			return err
		}
	}
	switch ev.Type {
	case stream.EventBeginObject:
		return enc.BeginObject()
	case stream.EventEndObject:
		return enc.EndObject()
	case stream.EventBeginArray:
		return enc.BeginArray()
	case stream.EventEndArray:
		return enc.EndArray()
	case stream.EventKey:
		return enc.WriteKey(ev.Key)
	case stream.EventIntKey:
		return enc.WriteIntKey(int(ev.IntKey))
	case stream.EventString:
		return enc.WriteString(ev.String)
	case stream.EventInt:
		return enc.WriteInt(ev.Int)
	case stream.EventFloat:
		return enc.WriteFloat(ev.Float)
	case stream.EventBool:
		return enc.WriteBool(ev.Bool)
	case stream.EventNull:
		return enc.WriteNull()
	case stream.EventHeadComment:
		return enc.WriteHeadComment(ev.CommentLines)
	case stream.EventLineComment:
		return enc.WriteLineComment(ev.CommentLines)
	}
	return fmt.Errorf("stream: event %v cannot be encoded", ev.Type)
}

// readValueAt answers the value at path as of commit, in the session's view, or a
// PathError saying why there is none. It is one read at the path (storage.Read), built
// under the session's budget; nothing wider is read on its behalf.
//
// Absence is classified without a wide read either. The PathError a client is owed says
// how far the path resolved and what was there instead -- a missing field under an object
// is ordinary, a field asked of a string is a disagreement about the document's shape --
// and that is a fact about the nearest ancestor that IS there, answered from its first
// event: an object, an array, a scalar. One event per ancestor tried, deepest first.
func (s *Session) readValueAt(path string, commit int64) (*ir.Node, error) {
	if commit == 0 {
		return nil, s.classifyAbsent(path, commit)
	}
	c, err := s.storage.Read(commit, s.scopeID(), path)
	if err != nil {
		return nil, err
	}
	if c.Presence() == storage.Absent {
		c.Close()
		return nil, s.classifyAbsent(path, commit)
	}
	node, err := storage.Collect(c, s.readBudget)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, s.classifyAbsent(path, commit)
	}
	// In the client's vocabulary: a keyed array is an array here and an object of names
	// in the store (storage.RaiseState).
	return s.storage.RaiseState(s.scopeID(), node, path, commit), nil
}

// classifyAbsent is the PathError for a path that resolves to nothing at commit.
func (s *Session) classifyAbsent(path string, commit int64) error {
	segs := kpath.SplitAll(path)
	for _, part := range segs {
		if p, err := kpath.Parse(part); err == nil && p != nil && p.Wild() {
			// A wildcard names a SET of values, and a read answers one: the caller's path
			// is wrong in a way no write can fix.
			return &PathError{Kind: PathBadSegment, Path: path, Segment: part}
		}
	}
	if len(segs) == 0 || commit == 0 {
		if len(segs) == 0 {
			return &PathError{Kind: PathAbsent, Path: path}
		}
		return &PathError{Kind: PathAbsent, Path: path, Segment: firstSegmentName(segs[0])}
	}
	resolvedNames := make([]string, len(segs))
	for i, part := range segs {
		if name, isField := kpath.SegmentFieldName(part); isField {
			resolvedNames[i] = name
		} else {
			resolvedNames[i] = part
		}
	}
	for depth := len(segs) - 1; depth >= 0; depth-- {
		prefix := joinKPathSegments(segs[:depth])
		c, err := s.storage.Read(commit, s.scopeID(), prefix)
		if err != nil {
			return err
		}
		if c.Presence() == storage.Absent {
			c.Close()
			continue
		}
		found := firstValueType(c)
		c.Close()
		seg := segs[depth]
		resolved := strings.Join(resolvedNames[:depth], ".")
		if _, isField := kpath.SegmentFieldName(seg); isField {
			if found != ir.ObjectType {
				return &PathError{Kind: PathTypeConflict, Path: path, Segment: seg, Resolved: resolved, Found: found}
			}
			return &PathError{Kind: PathAbsent, Path: path, Segment: resolvedNames[depth], Resolved: resolved}
		}
		if !segmentSuitsType(seg, found) {
			return &PathError{Kind: PathTypeConflict, Path: path, Segment: seg, Resolved: resolved, Found: found}
		}
		return &PathError{Kind: PathAbsent, Path: path, Segment: seg, Resolved: resolved}
	}
	return &PathError{Kind: PathAbsent, Path: path, Segment: firstSegmentName(segs[0])}
}

func firstSegmentName(seg string) string {
	if name, isField := kpath.SegmentFieldName(seg); isField {
		return name
	}
	return seg
}

// firstValueType is the kind of value a cursor holds, read from its first event past any
// head comment. The cursor is known to be present.
func firstValueType(c storage.Cursor) ir.Type {
	for {
		ev, err := c.Next()
		if err != nil {
			return ir.NullType
		}
		switch ev.Type {
		case stream.EventHeadComment, stream.EventLineComment:
			continue
		case stream.EventBeginObject:
			return ir.ObjectType
		case stream.EventBeginArray:
			return ir.ArrayType
		case stream.EventString:
			return ir.StringType
		case stream.EventInt, stream.EventFloat:
			return ir.NumberType
		case stream.EventBool:
			return ir.BoolType
		default:
			return ir.NullType
		}
	}
}

// segmentSuitsType says whether a non-field segment addresses the KIND of container found:
// an index wants an array, a sparse index an object or an array, a key an array.
func segmentSuitsType(seg string, found ir.Type) bool {
	p, err := kpath.Parse(seg)
	if err != nil || p == nil {
		return false
	}
	switch p.EntryKind() {
	case kpath.ArrayEntry:
		return found == ir.ArrayType
	case kpath.SparseArrayEntry:
		return found == ir.ObjectType || found == ir.ArrayType
	default:
		return found == ir.ArrayType
	}
}

func joinKPathSegments(segs []string) string {
	if len(segs) == 0 {
		return ""
	}
	result := segs[len(segs)-1]
	for i := len(segs) - 2; i >= 0; i-- {
		result = kpath.Join(segs[i], result)
	}
	return result
}

// scopedDocAt is the scoped value at path as of commit: the subtree there, or nil where
// the path resolves to nothing -- including at commit 0, where there is no document. The
// scoped watch compares presence as well as value, so absence is not turned into a null
// here (api/state.go).
func (s *Session) scopedDocAt(path string, commit int64) (*ir.Node, error) {
	if commit == 0 {
		return nil, nil
	}
	node, err := s.readValueAt(path, commit)
	var pe *PathError
	if errors.As(err, &pe) {
		return nil, nil
	}
	return node, err
}
