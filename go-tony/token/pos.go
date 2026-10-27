package token

import (
	"fmt"
	"sort"
	"strconv"
)

// PosDoc is the line table positions are reported against: the offsets of the
// newlines seen, from which a Pos works out its line and column when asked.
//
// In streaming mode the input is a stream of documents, and each document has a
// PosDoc of its own, starting at start, the absolute offset of its first byte
// (see Tokenizer.endLine and startDoc). Lines count from there -- for a
// bracketed value or scalar, from the line it opens on, since a head comment or
// tag on a line of its own ends that line too; for a block document, from the
// line after its `---` -- which is the only line number that means anything in
// a stream; offsets stay absolute. A single table for the whole stream kept 8
// bytes for every line that ever crossed a long-lived decoder
// (exkzk84wh12krt71w1n0).
type PosDoc struct {
	d     []byte
	n     []int
	start int
}

func (p *PosDoc) nl(i int) {
	if len(p.n) > 0 && p.n[len(p.n)-1] == i {
		return
	}
	// In streaming mode, p.d is empty - skip validation
	// In non-streaming mode, validate if document is available
	if len(p.d) > 0 {
		if i >= len(p.d) || p.d[i] != '\n' {
			panic("zsk")
		}
	}
	p.n = append(p.n, i)
}

func (p *PosDoc) LineCol(off int) (int, int) {
	N := len(p.n)
	di := sort.Search(N, func(i int) bool {
		return p.n[i] >= off
	})
	if di == 0 {
		return 0, off - p.start
	}
	return di, off - p.n[di-1] - 1
}

func (d *PosDoc) Pos(i int) *Pos {
	return &Pos{
		I: i,
		D: d,
	}
}

// PosWithContext creates a Pos with embedded context snippet.
// This allows Pos.String() to work without the full document.
// Parameters:
//   - absoluteOffset: absolute byte offset in the stream
//   - context: buffer slice containing bytes around the position
//   - bufferStartOffset: absolute offset where the context buffer starts
func (p *PosDoc) PosWithContext(absoluteOffset int, context []byte, bufferStartOffset int) *Pos {
	// Extract context snippet (e.g., 10 bytes before/after)
	contextStart := max(0, absoluteOffset-10)
	contextEnd := min(absoluteOffset+10, len(context)+bufferStartOffset)

	var contextBytes []byte
	if len(context) > 0 {
		relStart := max(0, contextStart-bufferStartOffset)
		relEnd := min(len(context), contextEnd-bufferStartOffset)
		if relStart < relEnd {
			contextBytes = context[relStart:relEnd]
		}
	}

	return &Pos{
		I:       absoluteOffset,
		D:       p,
		Context: contextBytes,
	}
}

// end is the position past the document's last byte; a streaming table has no
// document bytes, so there it is the document's start, never before it.
func (p *PosDoc) end() *Pos {
	return &Pos{
		I: max(len(p.d), p.start),
		D: p,
	}
}

type Pos struct {
	I       int
	D       *PosDoc
	Context []byte // context snippet around this position (for error messages)
}

func (p *Pos) LineCol() (int, int) {
	return p.D.LineCol(p.I)
}

func (p *Pos) Line() int {
	l, _ := p.LineCol()
	return l
}

func (p *Pos) Col() int {
	_, c := p.LineCol()
	return c
}

func (p Pos) String() string {
	var sample string
	if len(p.Context) > 0 {
		// Use stored context snippet (streaming mode)
		sample = string(p.Context)
	} else if p.D != nil && len(p.D.d) > 0 {
		// Fallback to full document if available (non-streaming mode)
		sample = string(p.D.d[max(0, p.I-5):min(p.I+5, len(p.D.d))])
	} else {
		sample = "?"
	}
	sample = strconv.Quote(sample)
	sample = sample[1 : len(sample)-1]
	return fmt.Sprintf("`...%s...` at offset %d (line=%d, col=%d)", sample, p.I, p.Line(), p.Col())
}
