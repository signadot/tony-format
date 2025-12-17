package snap

import "io"

type R interface {
	io.ReadSeekCloser
}

type W interface {
	io.WriteCloser
	io.Seeker
}
