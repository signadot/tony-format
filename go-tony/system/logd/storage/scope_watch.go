package storage

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/patches"
)

// What a BASELINE commit does to a scoped watch (scope_plan.md, phase 3).
//
// A scoped view is baseline with the scope's statements applied last, so a baseline delta
// cannot simply be folded into the value a scoped watch holds: where the scope has stated
// something, the fold would let baseline overwrite it. The footprint says where the scope
// has stated something, and that decides, with no entry read, which of three things a
// baseline delta is to the watch:
//
//	BaselineSteps     the scope has no live statement at, above or beneath any path the
//	                  delta states something at under the watched path. Nothing of the
//	                  scope's is displaced, and the delta folds into the held value.
//	BaselineHidden    a total cover of the scope stands at the watched path or above it.
//	                  Nothing baseline does at or beneath the path shows through; the
//	                  watch has nothing to say.
//	BaselineOverlaps  anything else: the delta meets a statement of the scope, and what
//	                  the watch holds afterwards is what a read at the path says.
//
// The scope's OWN commit is none of these: its delta applies last in the fold anyway, so
// it steps the held value as a baseline watch steps by baseline's.
type BaselineInScope int

const (
	BaselineSteps BaselineInScope = iota
	BaselineHidden
	BaselineOverlaps
)

func (v BaselineInScope) String() string {
	switch v {
	case BaselineHidden:
		return "hidden"
	case BaselineOverlaps:
		return "overlaps"
	}
	return "steps"
}

// BaselineDeltaInScope decides what a baseline delta, already projected to the watched
// path kp (at is what api.ProjectDelta answered there, and may be nil), is to a watch in
// the scope.
func (s *Storage) BaselineDeltaInScope(scope, kp string, at *ir.Node) BaselineInScope {
	foot := s.index.Footprint()
	if foot.TotallyCovered(scope, kp) {
		return BaselineHidden
	}
	overlaps, roots := false, 0
	patches.Roots(at, func(_ *ir.Node, rel string) {
		roots++
		if !overlaps && foot.Reaches(scope, kpath.Join(kp, rel)) {
			overlaps = true
		}
	})
	if roots == 0 && foot.Reaches(scope, kp) {
		overlaps = true
	}
	if overlaps {
		return BaselineOverlaps
	}
	return BaselineSteps
}
