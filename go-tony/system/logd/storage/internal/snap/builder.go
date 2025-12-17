package snap

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/stream"
)

// debugEnabled returns true if debug logging is enabled via SNAP_DEBUG env var
func debugEnabled() bool {
	return os.Getenv("SNAP_DEBUG") != ""
}

// debugLog prints debug messages if SNAP_DEBUG is enabled
func debugLog(format string, args ...interface{}) {
	if debugEnabled() {
		fmt.Printf("[SNAP_DEBUG] "+format+"\n", args...)
	}
}

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
	lastKey     *stream.Event
}

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

// WriteEvent processes a single event, writing it to the snapshot and updating state/index.
func (b *Builder) WriteEvent(ev *stream.Event) error {
	return b.onEvent(ev)
}

func (b *Builder) onEvent(ev *stream.Event) error {
	if err := b.state.ProcessEvent(ev); err != nil {
		return err
	}
	if !ev.IsValueStart() {
		if ev.Type == stream.EventKey || ev.Type == stream.EventIntKey {
			b.lastKey = ev
			return nil
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
	if b.lastKey != nil {
		if err := b.writeEvent(b.lastKey); err != nil {
			return err
		}
		b.lastKey = nil
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

func (b *Builder) writeEvent(ev *stream.Event) error {
	evD, err := ev.ToTony(gomap.EncodeWire(true))
	if err != nil {
		return err
	}
	evD = append(evD, '\n')
	eventSize := len(evD)

	_, err = b.w.Write(evD)
	if err != nil {
		return err
	}
	b.offset += int64(eventSize)
	b.chunkSize += eventSize
	return nil
}

func (b *Builder) Close() error {
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
