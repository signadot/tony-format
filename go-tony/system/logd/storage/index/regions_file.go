package index

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// The durable index (index_residency.md; rebuild_plan.md decision 6).
//
// Two files beside the logs. index.regions is APPEND-ONLY: a record per region, framed,
// holding that region's segments; a record is written when a region is persisted and is
// never changed, so a region that was written is readable for as long as the file holds
// it. index.manifest is small and written whole, atomically: which records are current,
// with each region's header -- its commit range, its count, its snapshots -- so that a
// store opens with the skeleton and every header and pages nothing in to do so. A record
// no manifest refers to is garbage, and the file is rewritten without its garbage when the
// store closes.
//
// Neither file is the record of what happened; the log is, and IndexFormatVersion, the
// generations the manifest carries, and any fault in either file each send the store back
// to it (storage.init, Build). What the files buy is a start that pages nothing and an
// eviction that can be undone.

// IndexFormatVersion is stamped on every manifest written, and required by every one
// loaded. Raise it when a defect makes previously written indexes untrustworthy, or when
// the layout changes; a load below it discards the files and rebuilds from the logs.
//
//	1  the commit tree could drop half a leaf on a duplicate insert
//	2  the fix for it
//	3  regions: index.regions and index.manifest replace index.gob
//	4  the footprint: a segment says whether it is a statement and what it covers, and the
//	   manifest holds every scope's live statements (footprint.go)
const IndexFormatVersion = 4

const (
	regionsFileName  = "index.regions"
	manifestFileName = "index.manifest"
)

// recordRef is where the regions file holds a region.
type recordRef struct {
	Off, Size int64
}

// regionsFile is the append-only record file.
type regionsFile struct {
	mu   sync.RWMutex
	f    *os.File
	path string
	size int64 // the append frontier
}

func openRegionsFile(dir string) (*regionsFile, error) {
	path := filepath.Join(dir, regionsFileName)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &regionsFile{f: f, path: path, size: st.Size()}, nil
}

// append writes one region record and answers where it is.
func (rf *regionsFile) append(segs []LogSegment) (*recordRef, error) {
	var body bytes.Buffer
	if err := gob.NewEncoder(&body).Encode(segs); err != nil {
		return nil, err
	}
	rec := make([]byte, 4+body.Len())
	binary.BigEndian.PutUint32(rec[:4], uint32(body.Len()))
	copy(rec[4:], body.Bytes())
	rf.mu.Lock()
	defer rf.mu.Unlock()
	off := rf.size
	if _, err := rf.f.WriteAt(rec, off); err != nil {
		return nil, err
	}
	rf.size += int64(len(rec))
	return &recordRef{Off: off, Size: int64(len(rec))}, nil
}

// read answers a record's segments.
func (rf *regionsFile) read(ref *recordRef) ([]LogSegment, error) {
	if ref == nil {
		return nil, errors.New("region has no record")
	}
	buf := make([]byte, ref.Size)
	rf.mu.RLock()
	_, err := rf.f.ReadAt(buf, ref.Off)
	rf.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(buf[:4])
	if int64(n)+4 != ref.Size {
		return nil, fmt.Errorf("region record at %d: frame says %d bytes, manifest says %d", ref.Off, n, ref.Size-4)
	}
	var segs []LogSegment
	if err := gob.NewDecoder(bytes.NewReader(buf[4:])).Decode(&segs); err != nil {
		return nil, err
	}
	return segs, nil
}

func (rf *regionsFile) sync() error {
	rf.mu.RLock()
	defer rf.mu.RUnlock()
	return rf.f.Sync()
}

func (rf *regionsFile) truncate(size int64) error {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if err := rf.f.Truncate(size); err != nil {
		return err
	}
	rf.size = size
	return nil
}

func (rf *regionsFile) close() error {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.f == nil {
		return nil
	}
	err := rf.f.Close()
	rf.f = nil
	return err
}

// Manifest says which records are current, and what each holds.
type Manifest struct {
	Version     int
	MaxCommit   int64            // every entry at or below it is described; Build catches up from it
	Generations map[string]int64 // log file -> generation when written; a mismatch means positions moved
	FileSize    int64            // the regions file's frontier when written; beyond it is torn
	Nodes       []ManifestNode
	Scopes      []ManifestScope // every scope's live statements (footprint.go)
}

type ManifestNode struct {
	Path    string
	Regions []ManifestRegion
}

type ManifestRegion struct {
	MinStart, MaxStart, MaxEnd, MaxTx int64
	Count                             int
	Off, Size                         int64
	Snaps                             []int64
}

func writeManifest(dir string, m *Manifest) error {
	path := filepath.Join(dir, manifestFileName)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(m); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func readManifest(dir string) (*Manifest, error) {
	f, err := os.Open(filepath.Join(dir, manifestFileName))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var m Manifest
	if err := gob.NewDecoder(f).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// OpenIndex opens the durable index in dir and answers the skeleton it describes, with
// every region cold, and the manifest it came from -- or a fresh, empty index and a nil
// manifest when there is nothing trustworthy to open: no files, a version this code does
// not vouch for, generations that disagree with the logs, or a file shorter than the
// manifest says. In every such case the caller rebuilds from the log, which is the record;
// the reason is answered so the caller can say it.
func OpenIndex(dir string, generation func(logFile string) int64) (idx *Index, m *Manifest, reason string, err error) {
	rf, err := openRegionsFile(dir)
	if err != nil {
		return nil, nil, "", err
	}
	res := &Residency{file: rf}
	fresh := func(why string) (*Index, *Manifest, string, error) {
		if err := rf.truncate(0); err != nil {
			return nil, nil, "", err
		}
		i := NewIndex("")
		i.res = res
		return i, nil, why, nil
	}
	m, err = readManifest(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fresh("no manifest")
	case err != nil:
		return fresh("manifest unreadable: " + err.Error())
	case m.Version != IndexFormatVersion:
		return fresh(fmt.Sprintf("manifest version %d, want %d", m.Version, IndexFormatVersion))
	case m.FileSize > rf.size:
		return fresh(fmt.Sprintf("regions file is %d bytes, manifest describes %d", rf.size, m.FileSize))
	}
	for logFile, gen := range m.Generations {
		if generation != nil && generation(logFile) != gen {
			return fresh(fmt.Sprintf("log %s is at generation %d, the index was written at %d", logFile, generation(logFile), gen))
		}
	}
	// Beyond the manifest's frontier is a record no manifest refers to: an append the
	// crash interrupted. It goes, so the next append starts where the manifest says.
	if err := rf.truncate(m.FileSize); err != nil {
		return nil, nil, "", err
	}
	idx = NewIndex("")
	idx.res = res
	for _, mn := range m.Nodes {
		node := idx.nodeAt(mn.Path)
		node.regions = node.regions[:0]
		for _, mr := range mn.Regions {
			node.regions = append(node.regions, &region{
				node:     node,
				minStart: mr.MinStart,
				maxStart: mr.MaxStart,
				maxEnd:   mr.MaxEnd,
				maxTx:    mr.MaxTx,
				count:    mr.Count,
				snaps:    append([]int64(nil), mr.Snaps...),
				rec:      &recordRef{Off: mr.Off, Size: mr.Size},
			})
		}
	}
	idx.foot.load(m.Scopes)
	return idx, m, "", nil
}

// nodeAt answers the node for a full path, creating the way down.
func (i *Index) nodeAt(path string) *Index {
	node := i
	for rest := path; rest != ""; {
		first, tail := kpath.Split(rest)
		node.Lock()
		child := node.Children[first]
		if child == nil {
			child = node.newChild(first)
			node.Children[first] = child
		}
		node.Unlock()
		node, rest = child, tail
	}
	return node
}

// Persist writes every region the file does not hold as it stands -- hot, or dirty -- and
// then the manifest. Regions written become evictable. It is what makes a ceiling
// enforceable: nothing is evicted that a page-in cannot get back, and this is what makes
// a region gettable. Called by the persister on its interval, and at close.
//
// It is not atomic against writers, and does not need to be: MaxCommit is taken before
// the pass, so an entry that lands during it is re-added by the catch-up at the next
// open, and a region written and then written to again is dirty again and written next
// time. A compaction that moves entries while this runs leaves records with positions the
// manifest's generations do not vouch for, and the next open rebuilds.
func (i *Index) Persist(generations map[string]int64) error {
	if i.res == nil || i.res.file == nil {
		return errors.New("index: no regions file to persist to")
	}
	maxCommit, _, _ := i.MaxCommit()
	var nodes []ManifestNode
	var walk func(node *Index) error
	walk = func(node *Index) error {
		if err := node.persistRegions(); err != nil {
			return err
		}
		if mn, ok := node.manifestNode(); ok {
			nodes = append(nodes, mn)
		}
		for _, c := range node.childrenOf() {
			if err := walk(c.index); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(i); err != nil {
		return err
	}
	if err := i.res.file.sync(); err != nil {
		return err
	}
	i.res.file.mu.RLock()
	size := i.res.file.size
	i.res.file.mu.RUnlock()
	m := &Manifest{
		Version:     IndexFormatVersion,
		MaxCommit:   maxCommit,
		Generations: generations,
		FileSize:    size,
		Nodes:       nodes,
		Scopes:      i.foot.snapshot(),
	}
	return writeManifest(filepath.Dir(i.res.file.path), m)
}

// persistRegions writes this node's regions the file does not hold as they stand.
func (i *Index) persistRegions() error {
	type pending struct {
		reg     *region
		segs    []LogSegment
		version uint64
	}
	i.Lock()
	// An emptied region has nothing to hold; it goes, and its range falls to its
	// predecessor. The first region stays, as the range below everything.
	kept := i.regions[:0]
	for k, reg := range i.regions {
		if reg.count == 0 && k > 0 {
			continue
		}
		kept = append(kept, reg)
	}
	i.regions = kept
	var todo []pending
	for _, reg := range i.regions {
		if reg.resident && (reg.dirty || reg.rec == nil) && reg.count > 0 {
			todo = append(todo, pending{reg: reg, segs: i.segmentsOf(reg), version: reg.version})
		}
	}
	i.Unlock()
	for _, p := range todo {
		ref, err := i.res.file.append(p.segs)
		if err != nil {
			return err
		}
		i.Lock()
		clean := p.reg.version == p.version && p.reg.node == i
		if clean {
			p.reg.rec = ref
			p.reg.dirty = false
		} else if p.reg.rec == nil {
			p.reg.rec = ref // stale, but a manifest entry needs one; it is dirty and will be rewritten
		}
		i.Unlock()
		if clean {
			i.res.list(p.reg)
		}
	}
	return nil
}

// manifestNode answers this node's manifest entry, under its lock; ok is false when it
// holds nothing the file can describe.
func (i *Index) manifestNode() (ManifestNode, bool) {
	i.RLock()
	defer i.RUnlock()
	mn := ManifestNode{Path: i.full}
	for _, reg := range i.regions {
		if reg.count == 0 || reg.rec == nil {
			continue
		}
		mn.Regions = append(mn.Regions, ManifestRegion{
			MinStart: reg.minStart, MaxStart: reg.maxStart, MaxEnd: reg.maxEnd, MaxTx: reg.maxTx,
			Count: reg.count, Off: reg.rec.Off, Size: reg.rec.Size,
			Snaps: append([]int64(nil), reg.snaps...),
		})
	}
	return mn, len(mn.Regions) > 0
}

// Rewrite copies every current record into a fresh regions file and drops the rest --
// the records earlier persists superseded -- then persists. It holds the file for the
// copy, so it is for close, or for a store that has grown far past what it describes.
func (i *Index) Rewrite(generations map[string]int64) error {
	if i.res == nil || i.res.file == nil {
		return errors.New("index: no regions file to rewrite")
	}
	rf := i.res.file
	tmpPath := rf.path + ".tmp"
	tmp, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	var size int64
	var fail error
	var walk func(node *Index)
	walk = func(node *Index) {
		if fail != nil {
			return
		}
		node.Lock()
		for _, reg := range node.regions {
			if reg.rec == nil || reg.count == 0 {
				continue
			}
			buf := make([]byte, reg.rec.Size)
			_, err := rf.f.ReadAt(buf, reg.rec.Off) // rf.mu is held by Rewrite for the whole copy
			if err == nil {
				_, err = tmp.WriteAt(buf, size)
			}
			if err != nil {
				fail = err
				break
			}
			reg.rec = &recordRef{Off: size, Size: reg.rec.Size}
			size += reg.rec.Size
		}
		node.Unlock()
		for _, c := range node.childrenOf() {
			walk(c.index)
		}
	}
	rf.mu.Lock() // no appends while records move
	walk(i)
	if fail == nil {
		fail = tmp.Sync()
	}
	if fail == nil {
		fail = tmp.Close()
	}
	if fail == nil {
		fail = os.Rename(tmpPath, rf.path)
	}
	if fail != nil {
		rf.mu.Unlock()
		tmp.Close()
		os.Remove(tmpPath)
		return fail
	}
	old := rf.f
	f, err := os.OpenFile(rf.path, os.O_RDWR, 0o644)
	if err != nil {
		rf.mu.Unlock()
		return err
	}
	rf.f, rf.size = f, size
	old.Close()
	rf.mu.Unlock()
	return i.Persist(generations)
}

// Close persists nothing and releases the file. Persist first.
func (i *Index) Close() error {
	if i.res == nil || i.res.file == nil {
		return nil
	}
	return i.res.file.close()
}
