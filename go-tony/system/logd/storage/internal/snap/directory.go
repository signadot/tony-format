package snap

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/stream"
)

// The directory: a snapshot is a value, and it is also a listing of every container in
// it. For each container the snapshot holds, the directory has a TABLE of the container's
// direct children, in the order the events have them -- which is name order, since logd
// sorts object keys -- each entry saying the child's segment, its kind, where its events
// are, and, for a container, where its own table is. A listing at a path reads that
// path's table from where it left off: a page costs the page, and finding where a name
// is costs a binary search over the table's slots, not a read of the table
// (3kgxprskh12krjrmndn0).
//
// A table is written when its container's last event has been -- the builder knows every
// child's offset and size by then -- into the directory region, which follows the events
// and precedes the index:
//
//	[header][events][directory][index]
//
// The header (HeaderSize bytes) is a magic, the events' size, the directory's size, the
// root table's offset within the directory (dirNone for a snapshot of a scalar), and the
// index's size. A snapshot written before the directory existed has the 12-byte header
// of LegacyHeaderSize, which the magic cannot be mistaken for: Open reads both.
//
// A table is
//
//	[count u32][entriesLen u32][entries][slots: u32 × count]
//
// and an entry is
//
//	[kind u8][segment: uvarint length, bytes][offset: uvarint, relative to the
//	container's own offset][size: uvarint][table: uvarint, containers only, relative
//	to the directory]
//
// A slot is an entry's offset from the start of the entries, so the i'th entry is found
// without reading the ones before it, which is what a seek by name binary-searches over.
// The search rests on the entries being in name order. That is the snapshot's contract,
// not an assumption: logd stores object keys in name order, and the builder refuses a
// child that arrives out of it (Builder.openChild), so a table is sorted by
// construction.
// Offsets are relative so that they are short: a child's offset is within its container,
// and a table's within the directory.
//
// An entry's offset is where the child's FIRST event is, counting the key and the head
// comments that introduce it, and its size runs to the next child's first event or the
// container's end event -- so a line comment after the value is inside the child's
// window, as it is inside the window a path read answers (PathFinder.FindEvents).

// Kind is what kind of value a directory entry names: what a listing says of it, and
// whether it has a table of its own.
type Kind uint8

const (
	KindObject Kind = iota
	KindSparseArray
	KindArray
	KindString
	KindNumber
	KindBool
	KindNull
)

func (k Kind) String() string {
	switch k {
	case KindObject:
		return "object"
	case KindSparseArray:
		return "sparse array"
	case KindArray:
		return "array"
	case KindString:
		return "string"
	case KindNumber:
		return "number"
	case KindBool:
		return "bool"
	case KindNull:
		return "null"
	}
	return fmt.Sprintf("kind(%d)", uint8(k))
}

// IsContainer says the value has children, and so a table.
func (k Kind) IsContainer() bool {
	return k == KindObject || k == KindSparseArray || k == KindArray
}

// Entry is one child of a container, as its table lists it.
type Entry struct {
	Segment string // the child's segment as the store spells it: a field (quoted as kpath quotes it), [i], {n}
	Kind    Kind
	Offset  int64 // the child's first event, relative to the start of the events
	Size    int64 // through the last event that is the child's

	table int64 // the child's table, relative to the directory; meaningful for a container
}

var (
	// ErrNoDirectory is a snapshot with no directory: one written before there was one.
	ErrNoDirectory = errors.New("snapshot has no directory")
	// ErrNotAContainer is a table asked of a value that has none.
	ErrNotAContainer = errors.New("not a container")
	// ErrNotFound is a table asked of a path the snapshot does not hold.
	ErrNotFound = errors.New("path not found")
)

// dirNone is the root table's offset in a snapshot whose root is a scalar.
const dirNone = ^uint64(0)

// --- writing ---

// dirEntry is an entry as the builder accumulates it, before its container's table is
// written.
type dirEntry struct {
	segment string
	kind    Kind
	offset  int64 // absolute within the events
	size    int64
	table   int64 // absolute within the directory
}

// writeTable encodes a container's table and appends it to w, answering the table's
// offset within the directory. containerOffset is the container's own first event,
// which the entries' offsets are relative to.
func writeTable(w *countingWriter, containerOffset int64, entries []dirEntry) (int64, error) {
	at := w.n
	var body bytes.Buffer
	slots := make([]uint32, len(entries))
	var tmp [binary.MaxVarintLen64]byte
	put := func(v uint64) {
		n := binary.PutUvarint(tmp[:], v)
		body.Write(tmp[:n])
	}
	for i, e := range entries {
		slots[i] = uint32(body.Len())
		body.WriteByte(byte(e.kind))
		put(uint64(len(e.segment)))
		body.WriteString(e.segment)
		put(uint64(e.offset - containerOffset))
		put(uint64(e.size))
		if e.kind.IsContainer() {
			put(uint64(e.table))
		}
	}
	var head [8]byte
	binary.BigEndian.PutUint32(head[0:4], uint32(len(entries)))
	binary.BigEndian.PutUint32(head[4:8], uint32(body.Len()))
	if _, err := w.Write(head[:]); err != nil {
		return 0, err
	}
	if _, err := w.Write(body.Bytes()); err != nil {
		return 0, err
	}
	slotBytes := make([]byte, 4*len(slots))
	for i, s := range slots {
		binary.BigEndian.PutUint32(slotBytes[4*i:], s)
	}
	if _, err := w.Write(slotBytes); err != nil {
		return 0, err
	}
	return at, nil
}

// countingWriter is a writer that knows how much it has written.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// --- reading ---

// directory locates the tables in an open snapshot.
type directory struct {
	r    io.ReadSeeker
	base int64 // the directory's absolute offset in r
	size int64
	root uint64 // the root table, relative to base; dirNone for none
}

// Table is one container's table, open for reading: Seek positions it, Next reads the
// entry at the position and advances, Entry reads one by index.
type Table struct {
	d               *directory
	at              int64 // the table's absolute offset
	count           int
	entriesAt       int64 // absolute offset of the entries
	slotsAt         int64 // absolute offset of the slots
	containerOffset int64 // the container's first event, which entries are relative to

	pos int // the index Next reads next
	buf *bufio.Reader
	// bufAt is the entry index buf is positioned at, or -1 when buf must be reopened.
	bufAt int
}

// Len is the number of entries.
func (t *Table) Len() int { return t.count }

// Entry reads the i'th entry.
func (t *Table) Entry(i int) (Entry, error) {
	if i < 0 || i >= t.count {
		return Entry{}, fmt.Errorf("entry %d of %d", i, t.count)
	}
	var slot [4]byte
	if _, err := t.d.r.Seek(t.slotsAt+4*int64(i), io.SeekStart); err != nil {
		return Entry{}, err
	}
	if _, err := io.ReadFull(t.d.r, slot[:]); err != nil {
		return Entry{}, err
	}
	if _, err := t.d.r.Seek(t.entriesAt+int64(binary.BigEndian.Uint32(slot[:])), io.SeekStart); err != nil {
		return Entry{}, err
	}
	t.bufAt = -1
	return t.readEntry(bufio.NewReaderSize(t.d.r, 256))
}

// Seek positions the table after the entry whose segment is `after`, or at the first
// entry whose segment sorts after it when there is none: a listing resumes from the last
// name it answered. "" is the start.
func (t *Table) Seek(after string) error {
	t.bufAt = -1
	if after == "" {
		t.pos = 0
		return nil
	}
	target, err := kpath.Parse(after)
	if err != nil || target == nil {
		return fmt.Errorf("seek %q: not a segment", after)
	}
	lo, hi := 0, t.count
	for lo < hi {
		mid := (lo + hi) / 2
		e, err := t.Entry(mid)
		if err != nil {
			return err
		}
		if compareSegments(e.Segment, target) <= 0 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	t.pos = lo
	return nil
}

// Next reads the entry at the position and advances; ok is false at the end.
func (t *Table) Next() (e Entry, ok bool, err error) {
	if t.pos >= t.count {
		return Entry{}, false, nil
	}
	if t.buf == nil || t.bufAt != t.pos {
		var slot [4]byte
		if _, err := t.d.r.Seek(t.slotsAt+4*int64(t.pos), io.SeekStart); err != nil {
			return Entry{}, false, err
		}
		if _, err := io.ReadFull(t.d.r, slot[:]); err != nil {
			return Entry{}, false, err
		}
		if _, err := t.d.r.Seek(t.entriesAt+int64(binary.BigEndian.Uint32(slot[:])), io.SeekStart); err != nil {
			return Entry{}, false, err
		}
		t.buf = bufio.NewReaderSize(t.d.r, 4096)
		t.bufAt = t.pos
	}
	e, err = t.readEntry(t.buf)
	if err != nil {
		t.bufAt = -1
		return Entry{}, false, err
	}
	t.pos++
	t.bufAt = t.pos
	return e, true, nil
}

// Child opens the table of a container entry read from this table.
func (t *Table) Child(e Entry) (*Table, error) {
	if !e.Kind.IsContainer() {
		return nil, fmt.Errorf("%s: %w", e.Segment, ErrNotAContainer)
	}
	return t.d.open(e.table, e.Offset)
}

func (t *Table) readEntry(r *bufio.Reader) (Entry, error) {
	kind, err := r.ReadByte()
	if err != nil {
		return Entry{}, err
	}
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return Entry{}, err
	}
	seg := make([]byte, n)
	if _, err := io.ReadFull(r, seg); err != nil {
		return Entry{}, err
	}
	off, err := binary.ReadUvarint(r)
	if err != nil {
		return Entry{}, err
	}
	size, err := binary.ReadUvarint(r)
	if err != nil {
		return Entry{}, err
	}
	e := Entry{Segment: string(seg), Kind: Kind(kind), Offset: t.containerOffset + int64(off), Size: int64(size)}
	if e.Kind.IsContainer() {
		tbl, err := binary.ReadUvarint(r)
		if err != nil {
			return Entry{}, err
		}
		e.table = int64(tbl)
	}
	return e, nil
}

// open reads a table's head at rel (relative to the directory) for the container whose
// first event is at containerOffset.
func (d *directory) open(rel, containerOffset int64) (*Table, error) {
	if rel < 0 || rel+8 > d.size {
		return nil, fmt.Errorf("table at %d is outside the directory of %d bytes", rel, d.size)
	}
	at := d.base + rel
	if _, err := d.r.Seek(at, io.SeekStart); err != nil {
		return nil, err
	}
	var head [8]byte
	if _, err := io.ReadFull(d.r, head[:]); err != nil {
		return nil, err
	}
	count := int(binary.BigEndian.Uint32(head[0:4]))
	entriesLen := int64(binary.BigEndian.Uint32(head[4:8]))
	return &Table{
		d: d, at: at, count: count,
		entriesAt: at + 8, slotsAt: at + 8 + entriesLen,
		containerOffset: containerOffset, bufAt: -1,
	}, nil
}

// compareSegments orders a segment against a parsed one as the store orders keys: as
// kpath compares single-segment paths, which is the order the index is searched in.
func compareSegments(seg string, target *kpath.KPath) int {
	kp, err := kpath.Parse(seg)
	if err != nil || kp == nil {
		return -1
	}
	return kp.Compare(target)
}

// Table answers the table of the container at p: the root's for "", descending a level
// per segment. ErrNoDirectory for a snapshot without one, ErrNotAContainer where p names
// a value with no children, ErrNotFound where it names nothing.
func (s *Snapshot) Table(p string) (*Table, error) {
	tb, _, err := s.TableOf(p)
	return tb, err
}

// TableOf is Table with the kind of the container at p. The root's kind is read off its
// table's first entry -- a position, a sparse key, or a field -- and an empty root is an
// object, which lists the same nothing either way.
func (s *Snapshot) TableOf(p string) (*Table, Kind, error) {
	if s.dir == nil {
		return nil, 0, ErrNoDirectory
	}
	if s.dir.root == dirNone {
		return nil, 0, fmt.Errorf("the root: %w", ErrNotAContainer)
	}
	tb, err := s.dir.open(int64(s.dir.root), 0)
	if err != nil {
		return nil, 0, err
	}
	kind := KindObject
	if tb.count > 0 {
		first, err := tb.Entry(0)
		if err != nil {
			return nil, 0, err
		}
		switch first.Segment[0] {
		case '[':
			kind = KindArray
		case '{':
			kind = KindSparseArray
		}
	}
	kp, err := kpath.Parse(p)
	if err != nil {
		return nil, 0, err
	}
	for x := kp; x != nil; x = x.Next {
		e, found, err := tb.find(dirSegment(x))
		if err != nil {
			return nil, 0, err
		}
		if !found {
			return nil, 0, fmt.Errorf("%q: %w", p, ErrNotFound)
		}
		if !e.Kind.IsContainer() {
			return nil, 0, fmt.Errorf("%q: %w", p, ErrNotAContainer)
		}
		if tb, err = tb.Child(e); err != nil {
			return nil, 0, err
		}
		kind = e.Kind
	}
	return tb, kind, nil
}

// find answers the entry whose segment is seg, by binary search.
func (t *Table) find(seg string) (Entry, bool, error) {
	target, err := kpath.Parse(seg)
	if err != nil || target == nil {
		return Entry{}, false, fmt.Errorf("%q: not a segment", seg)
	}
	lo, hi := 0, t.count
	for lo < hi {
		mid := (lo + hi) / 2
		e, err := t.Entry(mid)
		if err != nil {
			return Entry{}, false, err
		}
		switch c := compareSegments(e.Segment, target); {
		case c == 0:
			return e, true, nil
		case c < 0:
			lo = mid + 1
		default:
			hi = mid
		}
	}
	return Entry{}, false, nil
}

// ReadEntry reads the value an entry of the table at `parent` names: its events, decoded
// as a node with its own comments and without the key that introduces it. It is a read
// of one child that costs the child.
func (s *Snapshot) ReadEntry(parent string, e Entry) (*ir.Node, error) {
	if e.Offset < 0 || e.Offset+e.Size > int64(s.EventSize) {
		return nil, fmt.Errorf("entry %s: events %d+%d are outside the stream of %d", e.Segment, e.Offset, e.Size, s.EventSize)
	}
	if _, err := s.R.Seek(s.eventsAt+e.Offset, io.SeekStart); err != nil {
		return nil, err
	}
	raw := make([]byte, e.Size)
	if _, err := io.ReadFull(s.R, raw); err != nil {
		return nil, err
	}
	rd := bytes.NewReader(raw)
	var evs []stream.Event
	introduced := false // past the child's own key, which the caller has as the segment
	for rd.Len() > 0 {
		var ev stream.Event
		if err := ev.ReadBinary(rd); err != nil {
			return nil, fmt.Errorf("entry %s: %w", e.Segment, err)
		}
		if !introduced && (ev.Type == stream.EventKey || ev.Type == stream.EventIntKey) {
			continue
		}
		if ev.IsValueStart() {
			introduced = true
		}
		evs = append(evs, ev)
	}
	return stream.EventsToNode(evs)
}
