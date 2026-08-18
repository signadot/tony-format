package index

import (
	"bytes"
	"encoding/gob"
	"os"
)

// IndexMetadata contains metadata about the persisted index.
type IndexMetadata struct {
	MaxCommit int64 // Highest commit number in the index
	// Version is the format-and-correctness version of the file, not of its layout:
	// the layout has not changed. A file written by a version whose tree DROPPED
	// entries is not to be trusted however well it parses, and Version is how a load
	// tells. Absent (0) means "written before this existed", which is exactly the
	// range that cannot be trusted (kds4sx3bh12krdrkghn0).
	Version int
}

// IndexFormatVersion is stamped on every index written, and required by every index
// loaded. Raise it when a defect makes previously written indexes untrustworthy; a load
// below it discards the file and rebuilds from the logs, which is what the logs are for.
//
//	1  the commit tree could drop half a leaf on a duplicate insert
const IndexFormatVersion = 1

// IndexWithMetadata wraps an index with its metadata for persistence.
type IndexWithMetadata struct {
	Index    *Index
	Metadata IndexMetadata
}

// StoreIndex persists the index to the given path.
// It writes to a temporary file first and then atomically renames it to the target path.
func StoreIndex(path string, idx *Index) error {
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := gob.NewEncoder(f)
	if err := enc.Encode(idx); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// LoadIndex loads the index from the given path.
func LoadIndex(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var idx Index
	dec := gob.NewDecoder(f)
	if err := dec.Decode(&idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// StoreIndexWithMetadata persists the index along with metadata (max commit number).
// This allows incremental rebuilds on startup by scanning logs from MaxCommit + 1 forward.
func StoreIndexWithMetadata(path string, idx *Index, maxCommit int64) error {
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer f.Close()

	metadata := IndexMetadata{MaxCommit: maxCommit, Version: IndexFormatVersion}
	wrapper := IndexWithMetadata{
		Index:    idx,
		Metadata: metadata,
	}

	enc := gob.NewEncoder(f)
	if err := enc.Encode(wrapper); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// LoadIndexWithMetadata loads the index and the highest commit in it.
func LoadIndexWithMetadata(path string) (*Index, int64, error) {
	idx, meta, err := LoadIndexWithMeta(path)
	return idx, meta.MaxCommit, err
}

// LoadIndexWithMeta loads the index and all of its metadata, so a caller can decide
// whether to trust the file (see IndexMetadata.Version).
func LoadIndexWithMeta(path string) (*Index, IndexMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, IndexMetadata{}, err
	}
	defer f.Close()

	var wrapper IndexWithMetadata
	dec := gob.NewDecoder(f)
	if err := dec.Decode(&wrapper); err != nil {
		return nil, IndexMetadata{}, err
	}
	return wrapper.Index, wrapper.Metadata, nil
}

// GobEncode implements the gob.GobEncoder interface.
// It flattens the Index into a list of LogSegments for serialization.
//
// The lock is held to COPY this node's data and released before encoding it. It used
// to be held across the encode -- and since encoding a node encodes its children, the
// root's lock was held while the whole index was serialized, while every write takes
// that same lock to Add a segment. Every write during a persist waited for the entire
// index to be written out. On a store of fifty thousand commits that is seconds, it
// recurs every persist interval, and it lands on whichever write happens to be in
// flight: three unrelated sources all reporting "context deadline exceeded" on
// unremarkable paths at the same moment (v552mdbqh12kr7dtgdn0).
//
// The snapshot is therefore no longer atomic across the tree: a commit landing during
// the encode may appear in a child encoded after it and not in one encoded before.
// That is safe, and was already assumed -- the metadata's maxCommit is taken before
// the encode begins, and a restart replays the log from maxCommit+1 into the loaded
// index (see storage.loadIndex and index.Build), so anything captured beyond it is
// re-added, and Insert is a no-op for a segment already there.
func (i *Index) GobEncode() ([]byte, error) {
	// Tree is not serializable (it holds a Less function), so what is written is a
	// struct of the data: this node's key, its segments, and its children.
	type indexData struct {
		PathKey  string
		Segments []LogSegment
		Children map[string]*Index
	}

	i.RLock()
	data := indexData{
		PathKey:  i.PathKey,
		Children: make(map[string]*Index, len(i.Children)),
	}
	for k, child := range i.Children {
		data.Children[k] = child
	}
	i.Commits.Range(func(s LogSegment) bool {
		data.Segments = append(data.Segments, s)
		return true
	}, func(LogSegment) int { return 0 }) // 0 means all
	i.RUnlock()

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GobDecode implements the gob.GobDecoder interface.
func (i *Index) GobDecode(data []byte) error {
	type indexData struct {
		PathKey  string
		Segments []LogSegment
		Children map[string]*Index
	}

	var decoded indexData
	dec := gob.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&decoded); err != nil {
		return err
	}

	// Reconstruct the Index
	// We need to initialize the Tree with the comparison function.
	// Since we are decoding into an existing pointer (allocated by gob or caller),
	// we can't use NewIndex directly to replace 'i'.
	// We have to initialize fields.

	tmp := NewIndex(decoded.PathKey)
	i.PathKey = tmp.PathKey
	i.Commits = tmp.Commits
	i.Children = decoded.Children

	for _, s := range decoded.Segments {
		i.Commits.Insert(s)
	}

	return nil
}
