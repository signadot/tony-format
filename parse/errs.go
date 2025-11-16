package parse

import (
	"errors"
	"fmt"
)

var (
	errInternal = errors.New("internal parse error")
	ErrParse    = errors.New("parse error")
	ErrKeyTag   = fmt.Errorf("%w: key cannot be tagged", ErrParse)
)
