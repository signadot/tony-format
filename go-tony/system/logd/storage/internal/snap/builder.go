package snap

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/stream"
)

// Builder writes snapshot files by consuming stream events.
// Automatically creates index entries at chunk boundaries.
type Builder struct {
	w          W
	state      *stream.State
	offset     int64
	origOffset int64

	chunkSize int

	chunkPath   *string
	chunkOffset *int64
	index       *Index
	held        []*stream.Event

	// The directory (directory.go). A container's table is written when it closes, to a
	// scratch file, which Close copies in after the events: what is held for it is the
	// open containers' entries, and nothing else. dirRoot is the root's table when it is
	// written, dirNone until then and for a scalar root.
	dir     *countingWriter
	dirFile *os.File
	dirRoot uint64
	frames  []*dirFrame
	// heldAt is where the held events -- a key, head comments -- begin, which is where
	// the child they introduce begins; -1 when nothing is held.
	heldAt int64
}

// dirFrame is a container whose end has not been written: its own first event, the
// entries of its children so far, and what the next child is named.
type dirFrame struct {
	offset  int64
	kind    Kind
	entries []dirEntry
	open    bool   // the last entry's size is not yet known
	next    string // the segment of the child being introduced, from its key
	n       int    // children so far, which names a position in an array
}

// NewBuilder creates a snapshot builder writing to w.
// Populates the provided index as events are written.
// The snapshot begins at w's current position: NewBuilder reserves the header there,
// and [Builder.Close] fills it in.
func NewBuilder(w W, index *Index) (*Builder, error) {
	pos, err := w.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, HeaderSize)
	_, err = w.Write(buf)
	if err != nil {
		return nil, err
	}

	// Always add a root entry at offset 0 to ensure lookups have a fallback
	index.Entries = append(index.Entries, IndexEntry{
		Path:   nil,
		Offset: 0,
	})

	dirFile, err := os.CreateTemp("", "snapdir-*")
	if err != nil {
		return nil, fmt.Errorf("snapshot directory scratch: %w", err)
	}
	return &Builder{
		w:          w,
		state:      stream.NewState(),
		origOffset: pos,
		offset:     0, // Offset relative to start of event stream (after header)
		index:      index,
		dir:        &countingWriter{w: dirFile},
		dirFile:    dirFile,
		dirRoot:    dirNone,
		heldAt:     -1,
	}, nil
}

// WriteEvent writes an event to the snapshot.
// Creates index entries when chunk size threshold is reached.
func (b *Builder) WriteEvent(ev *stream.Event) error {
	return b.onEvent(ev)
}

func (b *Builder) onEvent(ev *stream.Event) error {
	if err := b.state.ProcessEvent(ev); err != nil {
		return err
	}
	if !ev.IsValueStart() {
		switch ev.Type {
		case stream.EventKey, stream.EventIntKey, stream.EventHeadComment:
			// Held until the value it introduces arrives, so that the chunk
			// beginning at that value begins at IT. A key was always held for this
			// reason. A head comment needs it for the same one and did not have
			// it, so its bytes fell before the index offset of the value it
			// describes -- not a read window that could be widened, but bytes on
			// the far side of the seek, unreachable from that offset
			// (3cdjz00jh12krns4g1n0).
			//
			// The child they introduce begins at the first of them, which is what
			// its directory entry records (directory.go).
			if b.heldAt < 0 {
				b.heldAt = b.offset
			}
			b.noteKey(ev)
			b.held = append(b.held, ev)
			return nil
		case stream.EventEndObject, stream.EventEndArray:
			// The container's table, before its end event is written.
			if err := b.closeContainer(); err != nil {
				return err
			}
		}
		// Anything else -- an end marker, a line comment -- belongs where it
		// stands, after the value it follows. Whatever is held goes out first:
		// the order of the stream is its meaning.
		if err := b.writeHeld(); err != nil {
			return err
		}
		// we just write non-key, non-value-starting events without tracking
		// size, to keep the chunks starting with a value or a key-value
		return b.writeEvent(ev)
	}
	// initialize chunk if not yet initialized
	// this will refer to the path after processing this event
	if b.chunkPath == nil {
		p := b.state.CurrentPath()
		b.chunkPath = &p
		tmp := b.offset
		b.chunkOffset = &tmp
	}
	// The child begins where its key or head comments did, or here.
	start := b.offset
	if b.heldAt >= 0 {
		start = b.heldAt
	}
	b.heldAt = -1
	b.openChild(ev, start)
	if err := b.writeHeld(); err != nil {
		return err
	}
	if err := b.writeEvent(ev); err != nil {
		return err
	}
	if b.chunkSize >= GetChunkSize() {
		if err := b.flushChunk(); err != nil {
			return err
		}
	}
	return nil
}

// writeHeld writes the events waiting for the value they introduce, in the order
// they arrived.
func (b *Builder) writeHeld() error {
	for _, held := range b.held {
		if err := b.writeEvent(held); err != nil {
			return err
		}
	}
	b.held = b.held[:0]
	return nil
}

func (b *Builder) writeEvent(ev *stream.Event) error {
	// Use compact binary encoding
	buf := &bytes.Buffer{}
	if err := ev.WriteBinary(buf); err != nil {
		return err
	}
	evD := buf.Bytes()
	eventSize := len(evD)

	_, err := b.w.Write(evD)
	if err != nil {
		return err
	}
	b.offset += int64(eventSize)
	b.chunkSize += eventSize
	return nil
}

// Close writes any events still held, then the index, fills in the header, and
// closes w.
func (b *Builder) Close() error {
	defer func() {
		if b.dirFile != nil {
			name := b.dirFile.Name()
			b.dirFile.Close()
			os.Remove(name)
		}
	}()
	// A document can end on a held event -- a comment after the last value, which
	// the format attributes to whatever comes next and nothing does. It is written
	// rather than dropped: the stream says what the document said.
	if err := b.writeHeld(); err != nil {
		return err
	}
	// Write final chunk to index if there's one pending
	if b.chunkSize != 0 {
		if err := b.flushChunk(); err != nil {
			return err
		}
	}

	// The directory after the events, copied in from the scratch file.
	if _, err := b.dirFile.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if n, err := io.Copy(b.w, b.dirFile); err != nil {
		return fmt.Errorf("snapshot directory: %w", err)
	} else if n != b.dir.n {
		return fmt.Errorf("snapshot directory: copied %d of %d bytes", n, b.dir.n)
	}

	id, err := b.index.ToTony(gomap.EncodeWire(true))
	if err != nil {
		return err
	}
	_, err = b.w.Write(id)
	if err != nil {
		return err
	}
	_, err = b.w.Seek(b.origOffset, io.SeekStart)
	if err != nil {
		return err
	}
	header := make([]byte, HeaderSize)
	copy(header[0:4], headerMagic[:])
	binary.BigEndian.PutUint64(header[4:12], uint64(b.offset))
	binary.BigEndian.PutUint64(header[12:20], uint64(b.dir.n))
	binary.BigEndian.PutUint64(header[20:28], b.dirRoot)
	binary.BigEndian.PutUint32(header[28:32], uint32(len(id)))
	_, err = b.w.Write(header)
	if err != nil {
		return err
	}
	return b.w.Close()
}

// noteKey remembers the segment a key introduces, for the entry of the value after it,
// and that a container with an int key is a sparse array.
func (b *Builder) noteKey(ev *stream.Event) {
	if len(b.frames) == 0 {
		return
	}
	f := b.frames[len(b.frames)-1]
	switch ev.Type {
	case stream.EventKey:
		f.next = kpath.Field(ev.Key).String()
	case stream.EventIntKey:
		f.next = kpath.SparseIndex(int(ev.IntKey)).String()
		f.kind = KindSparseArray
	}
}

// openChild records the child a value-start event begins, at start, in the enclosing
// container's frame, and opens a frame for it when it is a container itself. The root
// value has no enclosing frame and only opens one.
func (b *Builder) openChild(ev *stream.Event, start int64) {
	kind := kindOfEvent(ev)
	if n := len(b.frames); n > 0 {
		f := b.frames[n-1]
		b.closeEntry(f, start)
		seg := f.next
		if f.kind == KindArray {
			seg = kpath.Index(f.n).String()
		}
		f.entries = append(f.entries, dirEntry{segment: seg, kind: kind, offset: start})
		f.open = true
		f.next = ""
		f.n++
	}
	if kind.IsContainer() {
		b.frames = append(b.frames, &dirFrame{offset: start, kind: kind})
	}
}

// closeEntry gives the frame's open entry its size: it ends where the next thing begins.
func (b *Builder) closeEntry(f *dirFrame, at int64) {
	if f.open {
		e := &f.entries[len(f.entries)-1]
		e.size = at - e.offset
		f.open = false
	}
}

// closeContainer writes the table of the container whose end event is about to be
// written at b.offset, and records it in the enclosing frame's entry for the container.
func (b *Builder) closeContainer() error {
	n := len(b.frames)
	if n == 0 {
		return nil
	}
	f := b.frames[n-1]
	b.frames = b.frames[:n-1]
	b.closeEntry(f, b.offset)
	at, err := writeTable(b.dir, f.offset, f.entries)
	if err != nil {
		return fmt.Errorf("snapshot directory: %w", err)
	}
	if n == 1 {
		b.dirRoot = uint64(at)
		return nil
	}
	parent := b.frames[n-2]
	e := &parent.entries[len(parent.entries)-1]
	e.table = at
	e.kind = f.kind // an object is a sparse array once an int key has named a child
	return nil
}

// kindOfEvent is the kind of value an event begins. An object is an object until an int
// key says otherwise (noteKey).
func kindOfEvent(ev *stream.Event) Kind {
	switch ev.Type {
	case stream.EventBeginObject:
		return KindObject
	case stream.EventBeginArray:
		return KindArray
	case stream.EventString:
		return KindString
	case stream.EventInt, stream.EventFloat:
		return KindNumber
	case stream.EventBool:
		return KindBool
	}
	return KindNull
}

func (b *Builder) flushChunk() error {
	if b.chunkPath == nil {
		return nil
	}
	chunkPath := *b.chunkPath
	chunkOffset := *b.chunkOffset
	b.chunkPath = nil
	b.chunkOffset = nil
	b.chunkSize = 0
	kp, err := kpath.Parse(chunkPath)
	if err != nil {
		return err
	}
	if kp == nil {
		return nil
	}
	b.index.Entries = append(b.index.Entries, IndexEntry{
		Path:   &Path{KPath: *kp},
		Offset: chunkOffset,
	})
	return nil
}
