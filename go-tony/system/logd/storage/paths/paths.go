package paths

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

func FormatLogSegment(s *index.LogSegment, lvl int, pending bool) string {
	var base string
	if s.IsPoint() {
		if pending {
			// Pending files: just tx
			base = fmt.Sprintf("%s.%s", FormatLexInt(s.StartTx), segExt(pending))
		} else {
			// Committed point: commit-tx-level
			base = fmt.Sprintf("%s-%s-%d.%s", FormatLexInt(s.StartCommit), FormatLexInt(s.StartTx), lvl, segExt(pending))
		}
	} else {
		if pending {
			// Pending compacted: startTx-endTx-level
			base = fmt.Sprintf("%s-%s-%d.%s",
				FormatLexInt(s.StartTx),
				FormatLexInt(s.EndTx),
				lvl,
				segExt(pending))
		} else {
			// Compacted: commit.tx-commit.tx-level
			base = fmt.Sprintf("%s.%s-%s.%s-%d.%s",
				FormatLexInt(s.StartCommit),
				FormatLexInt(s.StartTx),
				FormatLexInt(s.EndCommit),
				FormatLexInt(s.EndTx),
				lvl,
				segExt(pending),
			)
		}
	}
	return path.Join(s.RelPath, base)
}

// ParseLogSegment gets an [index.LogSegment] and a compaction
// level from a filename path
func ParseLogSegment(p string) (*index.LogSegment, int, error) {
	dir, base := path.Split(p)
	// Trim trailing slash from dir
	dir = strings.TrimSuffix(dir, "/")

	ext := path.Ext(base)
	switch ext {
	case ".diff":
		base = strings.TrimSuffix(base, ".diff")
	case ".pending":
		base = strings.TrimSuffix(base, ".pending")

		// Check if it's a range (compacted) or single tx (point)
		if strings.Contains(base, "-") {
			// Compacted pending: startTx-endTx-level
			parts := strings.Split(base, "-")
			if len(parts) != 3 {
				return nil, 0, fmt.Errorf("invalid pending compacted format %q", base)
			}
			startTx, err := ParseLexInt(parts[0])
			if err != nil {
				return nil, 0, err
			}
			endTx, err := ParseLexInt(parts[1])
			if err != nil {
				return nil, 0, err
			}
			level, err := strconv.Atoi(parts[2])
			if err != nil {
				return nil, 0, err
			}

			return &index.LogSegment{
				StartCommit: 0,
				EndCommit:   0,
				StartTx:     startTx,
				EndTx:       endTx,
				RelPath:     dir,
			}, level, nil
		} else {
			// Point pending: just tx
			tx, err := ParseLexInt(base)
			if err != nil {
				return nil, 0, err
			}
			return index.PointLogSegment(0, tx, dir), 0, nil
		}
	default:
		return nil, 0, fmt.Errorf("unrecognized ext %q", path.Ext(base))
	}

	// not a pending compacted file
	parts := strings.Split(base, "-")
	if len(parts) != 2 && len(parts) != 3 {
		return nil, 0, fmt.Errorf("unrecognized base format %q", base)
	}
	level := 0
	if len(parts) == 3 {
		lvl, err := strconv.ParseUint(parts[2], 10, 32)
		if err != nil {
			return nil, 0, err
		}
		level = int(lvl)
	}
	b4, after := parts[0], parts[1]
	if strings.Contains(b4, ".") {
		res := &index.LogSegment{RelPath: dir}
		// compacted diff: commit.tx-commit.tx-level
		parts := strings.Split(b4, ".")
		if len(parts) != 2 {
			return nil, 0, fmt.Errorf("unrecognized base format %q", base)
		}
		startCom, startT, err := parseCommitTx(parts[0], parts[1])
		if err != nil {
			return nil, 0, err
		}
		res.StartCommit = startCom
		res.StartTx = startT

		// after is "commit.tx-level" or "commit.tx" (for backwards compat)
		afterParts := strings.Split(after, "-")
		if len(afterParts) == 2 {
			// New format: commit.tx-level
			level, err = strconv.Atoi(afterParts[1])
			if err != nil {
				return nil, 0, err
			}
			after = afterParts[0]
		}
		// after is now "commit.tx"
		parts = strings.Split(after, ".")
		if len(parts) != 2 {
			return nil, 0, fmt.Errorf("unrecognized base format %q", base)
		}
		endCom, endT, err := parseCommitTx(parts[0], parts[1])
		if err != nil {
			return nil, 0, err
		}
		res.EndCommit = endCom
		res.EndTx = endT
		return res, level, nil
	}
	commit, tx, err := parseCommitTx(b4, after)
	if err != nil {
		return nil, 0, err
	}
	return index.PointLogSegment(commit, tx, dir), level, nil
}

func FormatLexInt(v int64) string {
	d := strconv.FormatUint(uint64(v), 10)
	prefix := rune('a' + len(d) - 1)
	return string(prefix) + d
}

func ParseLexInt(v string) (int64, error) {
	if len(v) < 2 {
		return 0, fmt.Errorf("%q too short", v)
	}
	if 'a' <= v[0] && v[0] <= 's' {
		u, err := strconv.ParseUint(v[1:], 10, 64)
		if err != nil {
			return 0, err
		}
		return int64(u), nil

	}
	return 0, fmt.Errorf("invalid leading character in %q, expecting a-s", v)
}

func segExt(pending bool) string {
	if pending {
		return "pending"
	}
	return "diff"
}

func parseCommitTx(c, t string) (commit, tx int64, err error) {
	commit, err = ParseLexInt(c)
	if err != nil {
		return 0, 0, err
	}
	tx, err = ParseLexInt(t)
	if err != nil {
		return 0, 0, err
	}
	return
}
