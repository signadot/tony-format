package index

import (
	"cmp"
	"math"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

type Index struct {
	sync.RWMutex
	Name     string // eg "" for root
	Commits  *Tree[LogSegment]
	Children map[string]*Index // map from subdir names to sub indices
}

func NewIndex(name string) *Index {
	return &Index{
		Name: name,
		Commits: NewTree(func(a, b LogSegment) bool {
			if a.StartCommit < b.StartCommit {
				return true
			}
			if a.StartCommit > b.StartCommit {
				return false
			}
			if a.StartTx < b.StartTx {
				return true
			}
			if a.StartTx > b.StartTx {
				return false
			}
			if a.RelPath < b.RelPath {
				return true
			}
			return false
		}),
		Children: map[string]*Index{},
	}
}

func (i *Index) Add(seg *LogSegment) {
	i.Lock()
	defer i.Unlock()
	if seg.RelPath == "" {
		i.Commits.Insert(*seg)
		return
	}
	parts := splitPath(seg.RelPath)
	hd, rest := parts[0], parts[1:]
	restPath := strings.Join(rest, "/")
	child := i.Children[hd]
	if child == nil {
		child = NewIndex(hd)
		i.Children[hd] = child
	}
	orgPath := seg.RelPath
	seg.RelPath = restPath
	child.Add(seg)
	seg.RelPath = orgPath
}

func (i *Index) Remove(seg *LogSegment) bool {
	if seg.RelPath == "" {
		i.Lock()
		defer i.Unlock()
		return i.Commits.Remove(*seg)
	}
	parts := splitPath(seg.RelPath)
	hd, rest := parts[0], parts[1:]
	i.RLock()
	defer i.RUnlock()
	c := i.Children[hd]
	if c == nil {
		return false
	}
	orgPath := seg.RelPath
	seg.RelPath = strings.Join(rest, "/")
	res := c.Remove(seg)
	// nb low grade mem leak when c empty after remove
	seg.RelPath = orgPath
	return res
}

func (i *Index) LookupRange(vp string, from, to *int64) []LogSegment {
	i.RLock()
	defer i.RUnlock()
	res := []LogSegment{}
	i.Commits.Range(func(c LogSegment) bool {
		res = append(res, c)
		return true
	}, rangeFunc(from, to))
	if vp == "" {
		for _, ci := range i.Children {
			cRes := ci.LookupRange("", from, to)
			for j := range cRes {
				seg := &cRes[j]
				seg.RelPath = path.Join(ci.Name, seg.RelPath)
				res = append(res, *seg)
			}
		}
		slices.SortFunc(res, LogSegCompare)
		return res
	}
	parts := splitPath(vp)
	hd, rest := parts[0], parts[1:]
	c := i.Children[hd]
	if c == nil {
		return res
	}
	cRes := c.LookupRange(strings.Join(rest, "/"), from, to)
	for j := range cRes {
		seg := &cRes[j]
		seg.RelPath = path.Join(hd, seg.RelPath)
	}
	res = append(res, cRes...)
	slices.SortFunc(res, LogSegCompare)
	return res
}

// LogSegCompare compares 2 log segments by their
// start commit, start-tx, end-commit, end-tx, and path.
func LogSegCompare(a, b LogSegment) int {
	n := cmp.Compare(a.StartCommit, b.StartCommit)
	if n != 0 {
		return n
	}
	n = cmp.Compare(a.StartTx, b.StartTx)
	if n != 0 {
		return n
	}
	n = cmp.Compare(a.EndCommit, b.EndCommit)
	if n != 0 {
		return n
	}
	n = cmp.Compare(a.EndTx, b.EndTx)
	if n != 0 {
		return n
	}
	return cmp.Compare(a.RelPath, b.RelPath)
}

func rangeFunc(from, to *int64) func(LogSegment) int {
	start := int64(-1)
	if from != nil {
		start = *from
	}
	end := int64(math.MaxInt64)
	if to != nil {
		end = *to
	}
	return func(v LogSegment) int {
		if v.StartCommit < start {
			return -1
		}
		if v.StartCommit > end {
			return 1
		}
		return 0
	}
}

func splitPath(vp string) []string {
	if vp == "/" {
		panic("/")
	}
	return strings.Split(filepath.ToSlash(filepath.Clean(vp)), "/")
}

func optRange(oFrom, oTo *int64) (from, to int64) {
	from = -1
	if oFrom != nil {
		from = *oFrom
	}
	to = math.MaxInt64
	if oTo != nil {
		to = *oTo
	}
	return
}

// ListRange returns the immediate child names at this index level.
// Returns the keys of the Children map.
func (i *Index) ListRange(from, to *int64) []string {
	i.RLock()
	defer i.RUnlock()

	children := make([]string, 0, len(i.Children))
	for name, ci := range i.Children {
		ci.RLock()
		segs := ci.LookupRange("", from, to)
		ci.RUnlock()
		if len(segs) == 0 {
			continue
		}
		children = append(children, name)
	}
	return children
}
