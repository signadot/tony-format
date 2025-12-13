package snap

import "io"

type R interface {
	io.ReaderAt
	io.Closer
}

type W interface {
	io.WriteCloser
	io.Seeker
}
