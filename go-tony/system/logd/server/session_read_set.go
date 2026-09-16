package server

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/ident"
)

// A match whose path holds a wildcard names a SET, and this answers it: one result per
// node, each carrying that node's own path and the one commit the whole set is read at,
// then the marker that ends it (MatchResult).
//
// The walk is left to right. A concrete segment extends the path being built; a wildcard
// enumerates the children of the node the path has reached so far and recurs into each,
// so `a.*.b.*` is two levels of the same step. A branch where the rest of the path finds
// nothing contributes nothing: a query walks nodes of every kind, so a segment that does
// not fit the node it meets is a non-match rather than a fault -- which is what
// ir's own walk does for the same path (ir.Node.ListKPath).
//
// Members are answered by the code that answers a single-node read, so raising, the
// pattern, and the read budget are the same for a member as for a read of that member's
// path on its own. A second path that "also" answered a read is a second set of answers
// to keep in step; there is one.

// maxSetPage is the most members logd puts in one page, whatever a request asks for. A
// page is a unit of work the server chooses; the marker is what tells a client whether
// the set went on (MatchRequest.Limit).
const maxSetPage = 1000

// handleSetMatch answers a match whose path holds a wildcard.
func (s *Session) handleSetMatch(id *string, req *api.MatchRequest, path string, commit int64) {
	// A set answers paths and bodies unless the request asks for less.
	spec, err := api.ParseReturnSpec(req.Return, api.ReturnSpec{Path: true, Body: true})
	if err != nil {
		s.sendError(id, api.ErrCodeUnsupported, err.Error())
		return
	}

	limit := maxSetPage
	if req.Limit != nil {
		if *req.Limit <= 0 {
			s.sendError(id, api.ErrCodeInvalidPath,
				fmt.Sprintf("limit %d: a page holds at least one node", *req.Limit))
			return
		}
		limit = min(*req.Limit, maxSetPage)
	}

	// A continuation reads the commit its cursor names, not the current one: every page
	// of one paging read answers from the same state.
	after := ""
	if req.Cursor != "" {
		cur, err := decodeSetCursor(req.Cursor)
		if err != nil {
			s.sendError(id, api.ErrCodeInvalidPath, err.Error())
			return
		}
		if cur.path != path {
			s.sendError(id, api.ErrCodeInvalidPath, fmt.Sprintf(
				"cursor is for %q, and this asks about %q: a cursor continues the read that made it",
				cur.path, path))
			return
		}
		if req.Commit != nil && *req.Commit != cur.commit {
			s.sendError(id, api.ErrCodeCommitNotFound, fmt.Sprintf(
				"cursor reads at commit %d, and this asks for %d", cur.commit, *req.Commit))
			return
		}
		current, err := s.storage.GetCurrentCommit()
		if err != nil {
			s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to get current commit: %v", err))
			return
		}
		if cur.commit < 0 || cur.commit > current {
			s.sendError(id, api.ErrCodeCommitNotFound, fmt.Sprintf(
				"cursor reads at commit %d, which is outside [0, %d]", cur.commit, current))
			return
		}
		commit, after = cur.commit, cur.after
	}

	sent := 0
	last := ""
	more := false
	err = s.eachSetMember(path, commit, after, func(member string, proven bool) error {
		if sent == limit {
			// One member past the page: the set goes on, and the cursor resumes here.
			more = true
			return errPageFull
		}
		ok, err := s.sendSetMember(id, req, spec, member, commit, proven)
		if err != nil {
			return err
		}
		if ok {
			sent++
			last = member
		}
		return nil
	})
	if err != nil && err != errPageFull {
		s.sendReadError(id, err)
		return
	}
	cursor := ""
	if more {
		cursor = encodeSetCursor(setCursor{commit: commit, path: path, after: last})
	}
	s.send(api.NewMatchDoneResponse(id, commit, cursor))
}

// errPageFull stops the walk when a page is full. It never reaches a client: the walk
// is the only caller that raises it, and handleSetMatch is the only one that reads it.
var errPageFull = fmt.Errorf("page full")

// sendSetMember answers one member, and says whether it answered. A member that holds
// nothing, or that the request's pattern does not match, is not a member: it is skipped
// rather than sent as an absence, since a set says what IS there.
//
// proven says the walk has already seen this node -- it enumerated it -- so nothing has
// to be read to know it is there. That is what makes `return: paths` cheap: the names
// are what the walk found, so a set of ten thousand costs the walk rather than ten
// thousand reads.
func (s *Session) sendSetMember(id *string, req *api.MatchRequest, spec api.ReturnSpec, member string, commit int64, proven bool) (bool, error) {
	hasPattern := req.Data != nil && req.Data.Type != ir.NullType
	reportPath, reportName := member, memberID(member)
	if !spec.Path {
		reportPath = ""
	}
	if !spec.ID {
		reportName = ""
	}

	if !spec.Body && !hasPattern {
		// Nothing to read: the answer is where the node is, and there is no pattern
		// that would need the node to decide whether this is a member at all.
		if !proven {
			// A concrete segment after the wildcard: the walk NAMED this path rather
			// than finding it. Presence answers whether it is there without building
			// the node.
			ok, err := s.pathExists(member, commit)
			if err != nil || !ok {
				return false, err
			}
		}
		s.send(api.NewMatchMemberResponse(id, commit, reportPath, reportName, nil))
		return true, nil
	}

	// The same fast path a single-node read takes: no pattern and no keyed array to
	// raise means the body is encoded as it is read, and no node of it is built.
	if !hasPattern && !s.raises() {
		err := s.encodedMatch(id, member, commit, reportPath, reportName)
		if err == nil {
			return true, nil
		}
		if isAbsent(err) {
			return false, nil
		}
		return false, err
	}
	state, err := s.readValueAt(member, commit)
	if err != nil {
		if isAbsent(err) {
			return false, nil
		}
		return false, err
	}
	if hasPattern {
		filtered, err := filterState(state, req.Data)
		if err != nil {
			return false, fmt.Errorf("failed to apply match filter: %w", err)
		}
		if !patternSelected(state, filtered) {
			return false, nil
		}
		state = filtered
	}
	if !spec.Body {
		// The pattern needed the node; the caller did not ask to be sent it.
		state = nil
	}
	s.send(api.NewMatchMemberResponse(id, commit, reportPath, reportName, state))
	return true, nil
}

// memberID is the name a node lives under in its parent, as a value rather than as a
// path segment: a1 for a field, 0 for a position, 7 for a sparse key, and r1 for an
// element of a keyed array -- the identity it is addressed BY, not the (id=r1) the
// store spells its field with. What addresses the node is the path; this is what names
// it, and a caller that asked jobs.* has the rest already.
//
// An identity of several fields has no single value, so it answers with the name, which
// is the only short form that says which element it is.
func memberID(path string) string {
	segs := kpath.SplitAll(path)
	if len(segs) == 0 {
		return ""
	}
	last := segs[len(segs)-1]
	kp, err := kpath.Parse(last)
	if err != nil || kp == nil {
		return last
	}
	switch {
	case kp.Index != nil:
		return strconv.Itoa(*kp.Index)
	case kp.SparseIndex != nil:
		return strconv.Itoa(*kp.SparseIndex)
	case kp.Key != nil:
		return *kp.Key
	case kp.Field != nil:
		name, isName, err := ident.Parse(*kp.Field)
		if err != nil || !isName {
			return *kp.Field
		}
		if v, ok := name.KeyValue(); ok {
			return v
		}
		return name.Field()
	}
	return last
}

// pathExists says whether anything stands at path, without building the node there.
func (s *Session) pathExists(path string, commit int64) (bool, error) {
	if commit == 0 {
		return false, nil
	}
	c, err := s.storage.Read(commit, s.scopeID(), path)
	if err != nil {
		if isAbsent(err) {
			return false, nil
		}
		return false, err
	}
	defer c.Close()
	return c.Presence() != storage.Absent, nil
}

// patternSelected reads what the pattern left: whether this member belongs to the set.
//
// A single-node read answers the null FilterState returns for a node that does not
// match, because a client asked about one path and is owed an answer about it. A member
// is not owed one -- a set says what IS there -- so a member the pattern rejects is
// left out rather than sent as a null, and "every job that is done" answers the jobs
// that are done rather than one result per job.
//
// The null that means "no match" and a null that IS the value are the same node, so the
// state before the filter is what tells them apart. An array is filtered item by item,
// so what the pattern left is its items.
func patternSelected(before, after *ir.Node) bool {
	if after == nil {
		return false
	}
	if before != nil && before.Type == ir.ArrayType {
		return len(after.Values) > 0
	}
	if after.Type != ir.NullType {
		return true
	}
	return before != nil && before.Type == ir.NullType
}

// eachSetMember calls fn with the path of each member of the set path names at commit,
// in the store's own order, skipping everything up to and including after (a cursor's
// resume point, empty for the first page).
func (s *Session) eachSetMember(path string, commit int64, after string, fn func(member string, proven bool) error) error {
	segs := kpath.SplitAll(path)
	afterSegs := []string(nil)
	if after != "" {
		afterSegs = kpath.SplitAll(after)
	}
	return s.walkSet("", segs, afterSegs, commit, false, fn)
}

// walkSet extends prefix by segs. afterSegs, while non-nil, is the cursor's path: the
// walk is still on the branch the last page ended in, so a level skips the children
// before the cursor's own, descends into that one still on the branch, and takes every
// later sibling from the start. That is a seek rather than a rescan -- the members
// already answered are never read again.
// proven says the path so far was enumerated rather than merely named, so something
// stands there and a paths-only answer needs no read to say so.
func (s *Session) walkSet(prefix string, segs, afterSegs []string, commit int64, proven bool, fn func(string, bool) error) error {
	if len(segs) == 0 {
		if afterSegs != nil {
			// The cursor's own member: answered on the previous page.
			return nil
		}
		return fn(prefix, proven)
	}
	seg := segs[0]
	kp, err := kpath.Parse(seg)
	if err != nil {
		return err
	}
	if !kp.Wild() {
		// Spelled as the store spells it, as a single-node read's path is: an element
		// of a keyed array is addressed by its identity, whichever sugar the client
		// used, and a position on a keyed array is refused here as it is there.
		child, childSeg, err := s.canonicalChild(prefix, seg)
		if err != nil {
			return err
		}
		next := afterSegs
		if next != nil {
			if len(next) == 0 || next[0] != childSeg {
				// The branch the cursor is on diverges here, so this one is either
				// already answered or not on the cursor's path at all.
				return nil
			}
			next = next[1:]
		}
		// A step the walk TOOK rather than found: whether anything stands here is not
		// known until something reads it.
		return s.walkSet(child, segs[1:], next, commit, false, fn)
	}

	children, err := s.setChildren(prefix, kp, commit)
	if err != nil {
		return err
	}
	resuming := afterSegs != nil
	for _, child := range children {
		next := []string(nil)
		if resuming {
			if len(afterSegs) == 0 {
				return nil
			}
			if child != afterSegs[0] {
				continue // before the cursor's child: answered on an earlier page
			}
			// The cursor's own child: descend still on its branch, and take every
			// later sibling whole.
			next = afterSegs[1:]
			resuming = false
		}
		// Enumerated: this child is one the walk found in the node above it.
		if err := s.walkSet(kpath.Join(prefix, child), segs[1:], next, commit, true, fn); err != nil {
			return err
		}
	}
	return nil
}

// setChildren is the segments a wildcard reaches at prefix: the children of the node
// there, of the kind the wildcard names. A wildcard that meets a container of another
// kind reaches nothing, which is an answer and not a fault.
//
// The value is read in the client's vocabulary, as any read of that path would be, so a
// keyed array is an array here. (*) names its elements by identity, which is how the
// store spells them and how a client addresses them; [*] names the positions of a dense
// array, and names nothing in a keyed one, where identity replaces position.
func (s *Session) setChildren(prefix string, kp *kpath.KPath, commit int64) ([]string, error) {
	node, err := s.readValueAt(prefix, commit)
	if err != nil {
		if isAbsent(err) {
			return nil, nil
		}
		return nil, err
	}
	node = ir.Uncomment(node)
	if node == nil {
		return nil, nil
	}
	var out []string
	switch node.Type {
	case ir.ObjectType:
		for i := range node.Fields {
			f := node.Fields[i]
			switch {
			case kp.FieldAll:
				out = append(out, kpath.Field(f.String).String())
			case kp.SparseIndexAll:
				// A sparse array is an object whose keys are numbers, so {*} is the
				// number-keyed fields and nothing else.
				if f.Type == ir.NumberType && f.Int64 != nil {
					out = append(out, "{"+strconv.FormatInt(*f.Int64, 10)+"}")
				}
			}
		}
	case ir.ArrayType:
		identity := s.identityAt(prefix)
		switch {
		case kp.KeyAll:
			if len(identity) == 0 {
				return nil, nil // not a keyed array: (*) names nothing here
			}
			for _, elem := range node.Values {
				name, ok := ident.Of(ir.Uncomment(elem), identity)
				if !ok {
					// An element that does not carry its identity cannot be addressed,
					// and the store refuses to hold one, so this is not reachable from a
					// write that went through logd. Skipping it keeps a set of
					// addressable members rather than one a client cannot act on.
					continue
				}
				// As the store spells it: the name is the FIELD that holds the element,
				// and kpath quotes it, so the member is runs."(id=r1)" -- the path the
				// index is keyed by, and one a client can read on its own.
				out = append(out, kpath.Field(name.Field()).String())
			}
		case kp.IndexAll:
			if len(identity) > 0 {
				return nil, nil // keyed: identity replaces position
			}
			for i := range node.Values {
				out = append(out, "["+strconv.Itoa(i)+"]")
			}
		}
	}
	return out, nil
}

// identityAt is the identity fields the schema gives the array at path, or none. The
// schema's own path elides the names of keyed elements, which schemaPath does.
func (s *Session) identityAt(path string) []string {
	schema := s.storage.SchemaFor(s.scopeID())
	if schema == nil {
		return nil
	}
	return schema.Identity(schemaPath(path))
}

// canonicalChild extends prefix by one concrete segment, spelled as the store spells
// it, and answers the path and that last segment -- the segment because it is what a
// cursor's path is compared against, and both have been through here.
func (s *Session) canonicalChild(prefix, seg string) (path, last string, err error) {
	canon, err := ident.CanonicalPath(s.storage.SchemaFor(s.scopeID()), kpath.Join(prefix, seg))
	if err != nil {
		return "", "", err
	}
	segs := kpath.SplitAll(canon)
	if len(segs) == 0 {
		return canon, "", nil
	}
	return canon, segs[len(segs)-1], nil
}

// isAbsent says the error is "nothing is there", which for a member of a set is not an
// error at all: the set says what IS there, so a branch holding nothing contributes
// nothing to it.
func isAbsent(err error) bool {
	var pe *PathError
	if errors.As(err, &pe) {
		return pe.Kind == PathAbsent
	}
	return errors.Is(err, ErrPathNotFound)
}

// setCursor is what a cursor says: the commit the set is being read at, the path the
// read started from, and the last member answered. It is opaque on the wire -- encoded
// so that a client reads it back to the server rather than reading it.
type setCursor struct {
	commit int64
	path   string
	after  string
}

func encodeSetCursor(c setCursor) string {
	raw := strconv.FormatInt(c.commit, 10) + "\x00" + c.path + "\x00" + c.after
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeSetCursor(s string) (setCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return setCursor{}, fmt.Errorf("cursor is not one this server wrote")
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 3 {
		return setCursor{}, fmt.Errorf("cursor is not one this server wrote")
	}
	commit, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return setCursor{}, fmt.Errorf("cursor is not one this server wrote")
	}
	return setCursor{commit: commit, path: parts[1], after: parts[2]}, nil
}
