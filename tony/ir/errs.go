package ir

import (
	"errors"

	"github.com/signadot/tony-format/tony/format"
)

var (
	errInternal = errors.New("internal error")

	ErrParse     = errors.New("parse error")
	ErrBadFormat = format.ErrBadFormat
)
