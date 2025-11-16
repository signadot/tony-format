package token

import (
	"fmt"
	"sort"
	"strconv"
)

type PosDoc struct {
	d []byte
	n []int
}

func (p *PosDoc) nl(i int) {
	if len(p.n) > 0 && p.n[len(p.n)-1] == i {
		return
	}
	p.n = append(p.n, i)
	if p.d[i] != '\n' {
		panic("zsk")
	}
}

func (p *PosDoc) LineCol(off int) (int, int) {
	N := len(p.n)
	di := sort.Search(N, func(i int) bool {
		return p.n[i] >= off
	})
	switch di {
	case 0:
		return 0, off
	case N:
		if N != 0 {
			return di, off - p.n[di-1] - 1
		}
		return 0, off
	default:
		return di, off - p.n[di-1] - 1
	}
}

func (d *PosDoc) Pos(i int) *Pos {
	return &Pos{
		I: i,
		D: d,
	}
}

func (p *PosDoc) end() *Pos {
	return &Pos{
		I: len(p.d),
		D: p,
	}
}

type Pos struct {
	I int
	D *PosDoc
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
	sample := string(p.D.d[max(0, p.I-5):min(p.I+5, len(p.D.d))])
	sample = strconv.Quote(sample)
	sample = sample[1 : len(sample)-1]
	return fmt.Sprintf("`...%s...` at offset %d (line=%d, col=%d)", sample, p.I, p.Line(), p.Col())
}
