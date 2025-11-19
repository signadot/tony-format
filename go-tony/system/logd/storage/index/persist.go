package index

import (
	"bytes"
	"encoding/gob"
	"os"
)

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

// GobEncode implements the gob.GobEncoder interface.
// It flattens the Index into a list of LogSegments for serialization.
func (i *Index) GobEncode() ([]byte, error) {
	// Collect all segments from this index and its children
	// Actually, we can just serialize the Name and the list of local segments, and the map of children.
	// But Tree is not serializable because of the Less function.
	// So we'll serialize a struct that holds the data.

	type indexData struct {
		Name     string
		Segments []LogSegment
		Children map[string]*Index
	}

	data := indexData{
		Name:     i.Name,
		Children: i.Children,
	}

	i.Commits.Range(func(s LogSegment) bool {
		data.Segments = append(data.Segments, s)
		return true
	}, func(LogSegment) int { return 0 }) // 0 means all

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
		Name     string
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

	tmp := NewIndex(decoded.Name)
	i.Name = tmp.Name
	i.Commits = tmp.Commits
	i.Children = decoded.Children

	for _, s := range decoded.Segments {
		i.Commits.Insert(s)
	}

	return nil
}
