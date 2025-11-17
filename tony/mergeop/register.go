package mergeop

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	mu sync.RWMutex
	d  = map[string]Symbol{}
)

var ErrSymbolExists = errors.New("symbol exists")

func Register(s Symbol) error {
	key := s.String()
	if strings.Contains(key, ".") {
		return fmt.Errorf("symbol %q must not contain '.'", key)
	}
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
	Register(And())
	Register(Or())
	Register(Not())
	Register(Glob())
	Register(Field())
	Register(Tag())
	Register(Type())
	Register(Subtree())
	Register(Nullify())
	Register(JSONPatch())
	Register(KeyedList())
	Register(All())
	Register(If())
	Register(Pass())
	Register(Quote())
	Register(Unquote())
	Register(Dive())
	Register(Embed())
	Register(Pipe())

	// tags from diffs
	Register(Insert())
	Register(Delete())
	Register(Replace())
	Register(Rename())
	Register(StrDiff())
	Register(ArrayDiff())
	Register(AddTag())
	Register(RemoveTag())
	Register(Retag())
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
