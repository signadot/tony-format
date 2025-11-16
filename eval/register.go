package eval

import (
	"errors"
	"fmt"
	"sync"
)

var (
	mu sync.RWMutex
	d  = map[string]Symbol{}
)

var ErrSymbolExists = errors.New("symbol exists")

func Register(s Symbol) error {
	mu.Lock()
	defer mu.Unlock()
	_, present := d[s.String()]
	if present {
		return fmt.Errorf("%s: %w", s, ErrSymbolExists)
	}
	d[s.String()] = s
	return nil
}

func init() {
	Register(Eval())
	Register(File())
	Register(Exec())
	Register(ToString())
	Register(ToInt())
	Register(ToValue())
	Register(B64Enc())
	Register(Script())
	Register(OSEnv())
}

func Lookup(s string) Symbol {
	mu.RLock()
	defer mu.RUnlock()
	return d[s]
}

func Symbols() []Symbol {
	mu.RLock()
	defer mu.RUnlock()
	res := make([]Symbol, 0, len(d))
	for _, s := range d {
		res = append(res, s)
	}
	return res
}
