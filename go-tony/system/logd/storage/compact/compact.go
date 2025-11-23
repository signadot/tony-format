package compact

import (
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/seq"
)

type Compactor struct {
	Config Config
	*seq.Seq
	Index *index.Index
	// Level is the indication of the the exponential backoff level
	Level int
	// HeadChan is a channel for receiving files representing index.LogSegment's
	HeadChan chan string
	curHead  string
}

func NewCompactor(cfg *Config, seq *seq.Seq, idx *index.Index) *Compactor {
	return &Compactor{
		Config:   *cfg,
		Seq:      seq,
		Index:    idx,
		HeadChan: make(chan string, 1),
		curHead:  -1,
	}
}
