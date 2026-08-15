package snap

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
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
	patches    []*ir.Node

	chunkSize int

	chunkPath   *string
	chunkOffset *int64
	index       *Index
	held        []*stream.Event
}

// NewBuilder creates a snapshot builder writing to w.
// Populates the provided index as events are written.
func NewBuilder(w W, index *Index, patches []*ir.Node) (*Builder, error) {
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

	return &Builder{
		w:          w,
		state:      stream.NewState(),
		origOffset: pos,
		offset:     0, // Offset relative to start of event stream (after header)
		index:      index,
		patches:    patches,
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
			b.held = append(b.held, ev)
			return nil
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

func (b *Builder) Close() error {
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
	binary.BigEndian.PutUint64(header[0:8], uint64(b.offset))
	binary.BigEndian.PutUint32(header[8:12], uint32(len(id)))
	_, err = b.w.Write(header)
	if err != nil {
		return err
	}
	return b.w.Close()
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
