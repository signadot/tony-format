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
	err = s.eachSetMember(path, commit, after, func(member string, proven bool, kind string) error {
		if sent == limit {
			// One member past the page: the set goes on, and the cursor resumes here.
			more = true
			return errPageFull
		}
		ok, err := s.sendSetMember(id, req, spec, member, commit, proven, kind)
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
// to be read to know it is there, and kind is what it saw, as MatchResult.IterType names
// it; "" for a node the walk named rather than found. That is what makes `return: path`
// and `return: iterType` cost the listing: the names and kinds are what the walk found,
// so a set of ten thousand costs the pages rather than ten thousand reads.
func (s *Session) sendSetMember(id *string, req *api.MatchRequest, spec api.ReturnSpec, member string, commit int64, proven bool, kind string) (bool, error) {
	hasPattern := req.Data != nil && req.Data.Type != ir.NullType
	reportPath, reportName, reportIterType := member, memberID(member), ""
	if !spec.Path {
		reportPath = ""
	}
	if !spec.ID {
		reportName = ""
	}
	if spec.IterType {
		if kind == "" {
			// A concrete segment after the wildcard: the member's first event says what
			// it is, and nothing there is no member.
			k, err := s.iterTypeAt(member, commit)
			if err != nil || k == "" {
				return false, err
			}
			kind = k
		}
		reportIterType = kind
	}

	if !spec.Body && !hasPattern {
		// Nothing to read: the answer is where the node is, and there is no pattern
		// that would need the node to decide whether this is a member at all.
		if !proven && !spec.IterType {
			// A concrete segment after the wildcard: the walk NAMED this path rather
			// than finding it. Presence answers whether it is there without building
			// the node -- and an iterType has answered it already.
			ok, err := s.pathExists(member, commit)
			if err != nil || !ok {
				return false, err
			}
		}
		s.send(api.NewMatchMemberResponse(id, commit, reportPath, reportName, reportIterType, nil))
		return true, nil
	}

	// The same fast path a single-node read takes: no pattern and no keyed array to
	// raise means the body is encoded as it is read, and no node of it is built.
	if !hasPattern && !s.raisesAt(commit) {
		err := s.encodedMatch(id, member, commit, reportPath, reportName, reportIterType)
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
	s.send(api.NewMatchMemberResponse(id, commit, reportPath, reportName, reportIterType, state))
	return true, nil
}

// memberID is the name a node lives under in its parent, as the segment a client would
// write for it: a1 (or "a b") for a field, [0] for a position, {7} for a sparse key, and
// (id=r1) for an element of a keyed array -- the identity bound to its field, not the
// "(id=r1)" field the store keeps it under. So the id says what kind of child it is, and
// for an element which fields identify it, with no schema needed to read it; and a caller
// that asked jobs.* appends it to the prefix to address the node (eavavw16h12kst8dndn0).
//
// A name no key segment can carry (ident.Name.Key) answers with the stored field, quoted,
// which is still a segment a client can write.
func memberID(path string) string {
	segs := kpath.SplitAll(path)
	if len(segs) == 0 {
		return ""
	}
	last := segs[len(segs)-1]
	kp, err := kpath.Parse(last)
	if err != nil || kp == nil || kp.Field == nil {
		return last
	}
	if name, isName, err := ident.Parse(*kp.Field); err == nil && isName {
		if key, ok := name.Key(); ok {
			return key
		}
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
func (s *Session) eachSetMember(path string, commit int64, after string, fn func(member string, proven bool, kind string) error) error {
	segs := kpath.SplitAll(path)
	afterSegs := []string(nil)
	if after != "" {
		afterSegs = kpath.SplitAll(after)
	}
	return s.walkSet("", segs, afterSegs, commit, false, "", fn)
}

// walkSet extends prefix by segs. afterSegs, while non-nil, is the cursor's path: the
// walk is still on the branch the last page ended in, so a level descends into the
// cursor's own child still on the branch, and then lists every later sibling, from that
// child on. That is a seek rather than a rescan -- the members already answered are
// never read again, and the level's listing starts where the page ended.
//
// proven says the path so far was enumerated rather than merely named, so something
// stands there and a paths-only answer needs no read to say so; kind is what the
// enumeration saw there, "" when it was named.
//
// A level is enumerated as it is listed (storage.Children): each child is walked
// inside the listing's callback, so what is held at a level is the listing's own
// position and not the level's children, and a page of a set of ten thousand costs the
// page.
func (s *Session) walkSet(prefix string, segs, afterSegs []string, commit int64, proven bool, kind string, fn func(string, bool, string) error) error {
	if len(segs) == 0 {
		if afterSegs != nil {
			// The cursor's own member: answered on the previous page.
			return nil
		}
		return fn(prefix, proven, kind)
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
		child, childSeg, err := s.canonicalChild(prefix, seg, commit)
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
		return s.walkSet(child, segs[1:], next, commit, false, "", fn)
	}

	// The cursor's own child first, still on its branch: what is under it after the
	// cursor is this page's to answer.
	after := ""
	if afterSegs != nil {
		if len(afterSegs) == 0 {
			return nil
		}
		after = afterSegs[0]
		if err := s.walkSet(kpath.Join(prefix, after), segs[1:], afterSegs[1:], commit, true, "", fn); err != nil {
			return err
		}
	}
	// Then every later sibling, whole. Enumerated: each is one the listing found in the
	// node above it.
	var walkErr error
	err = s.eachChild(prefix, kp, commit, after, func(child, kind string) bool {
		walkErr = s.walkSet(kpath.Join(prefix, child), segs[1:], nil, commit, true, kind, fn)
		return walkErr == nil
	})
	if walkErr != nil {
		return walkErr
	}
	return err
}

// eachChild lists the segments a wildcard reaches at prefix, from after, with the kind
// of each as MatchResult.IterType names it: the children of the node there, of the kind
// the wildcard names. A wildcard that meets a container of another kind reaches nothing,
// which is an answer and not a fault.
//
// The store keeps a keyed array as an object of names, and (*) names those -- how the
// store spells the elements and how a client addresses them -- while .* and [*] name
// nothing there: to a client it is an array, and identity replaces position. Whether
// the array is keyed is the schema's word at the commit read (identityAt).
func (s *Session) eachChild(prefix string, kp *kpath.KPath, commit int64, after string, fn func(child, kind string) bool) error {
	keyed := len(s.identityAt(prefix, commit)) > 0
	switch {
	case kp.KeyAll && !keyed, kp.FieldAll && keyed, kp.IndexAll && keyed:
		return nil
	}
	return s.storage.Children(commit, s.scopeID(), prefix, after, func(c storage.Child) bool {
		var wanted bool
		switch c.Segment[0] {
		case '[':
			wanted = kp.IndexAll
		case '{':
			wanted = kp.SparseIndexAll
		default:
			wanted = kp.FieldAll || kp.KeyAll
		}
		if !wanted {
			return true
		}
		return fn(c.Segment, s.iterTypeOf(c.Kind, kpath.Join(prefix, c.Segment), commit))
	})
}

// iterTypeOf is a listed child's kind as MatchResult.IterType names it: an object the
// schema keys at the commit read is a KeyedArray.
func (s *Session) iterTypeOf(kind storage.ChildKind, path string, commit int64) string {
	switch kind {
	case storage.ChildObject:
		if s.storage.KeyedAt(s.scopeID(), path, commit) {
			return api.IterKeyedArray
		}
		return api.IterObject
	case storage.ChildSparseArray:
		return api.IterSparseArray
	case storage.ChildArray:
		return api.IterArray
	case storage.ChildString:
		return api.IterString
	case storage.ChildNumber:
		return api.IterNumber
	case storage.ChildBool:
		return api.IterBool
	}
	return api.IterNull
}

// identityAt is the identity fields the schema gives the array at path, or none. The
// schema's own path elides the names of keyed elements, which schemaPath does.
//
// It is the schema in force at commit, not now: that is the one the read at commit raises
// its arrays by, so an array that was keyed then is listed by (*) when read then, whatever
// the schema has said since.
func (s *Session) identityAt(path string, commit int64) []string {
	schema := s.storage.SchemaForAt(s.scopeID(), commit)
	if schema == nil {
		return nil
	}
	return schema.Identity(schemaPath(path))
}

// canonicalChild extends prefix by one concrete segment, spelled as the store spells
// it at commit, and answers the path and that last segment -- the segment because it is
// what a cursor's path is compared against, and both have been through here.
func (s *Session) canonicalChild(prefix, seg string, commit int64) (path, last string, err error) {
	canon, err := ident.CanonicalPath(s.storage.SchemaForAt(s.scopeID(), commit), kpath.Join(prefix, seg))
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
