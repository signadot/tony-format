package snap

import (
	"encoding/binary"
	"io"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/stream"
)

type Builder struct {
	w          W
	state      *stream.State
	offset     int64
	origOffset int64
	patches    []*ir.Node

	chunkSize int

	chunkPath *string
	index     *Index
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

	return &Builder{
		w:          w,
		state:      stream.NewState(),
		origOffset: pos,
		index:      index,
	}, nil
}

// WriteEvent processes a single event, writing it to the snapshot and updating state/index.
func (b *Builder) WriteEvent(ev *stream.Event) error {
	return b.onEvent(ev)
}

func (b *Builder) Close() error {
	id, err := b.index.ToTony()
	if err != nil {
		return err
	}
	_, err = b.w.Write(id)
	if err != nil {
		return nil
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

func (b *Builder) onEvent(ev *stream.Event) error {
	evD, err := ev.ToTony(gomap.EncodeWire(true))
	if err != nil {
		return err
	}
	_, err = b.w.Write(evD)
	if err != nil {
		return err
	}
	evPos := b.offset
	b.offset += int64(len(evD))
	b.chunkSize += len(evD)

	if err = b.state.ProcessEvent(ev); err != nil {
		return err
	}
	switch ev.Type {
	case stream.EventEndArray, stream.EventEndObject, stream.EventKey:
		return nil
	default:
	}

	p := b.state.CurrentPath()
	if b.chunkPath == nil {
		b.chunkPath = &p
	}

	if b.chunkSize < MaxChunkSize {
		return nil
	}

	if b.chunkPath == nil {
		return nil
	}
	chunkPath := *b.chunkPath
	b.chunkPath = nil
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
		Offset: evPos,
	})

	return nil
}
