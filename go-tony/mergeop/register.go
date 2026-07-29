package mergeop

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/signadot/tony-format/go-tony/libdiff"
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
	// libdiff cannot import mergeop, but a diff has to know which tags name
	// operations in order to escape the ones a document holds as data.
	libdiff.IsOp = func(tag string) bool {
		if len(tag) == 0 || tag[0] != '!' {
			return false
		}
		return Lookup(tag[1:]) != nil
	}

	Register(And())
	Register(Or())
	Register(Not())
	Register(Glob())
	Register(Field())
	Register(Tag())
	Register(Type())
	Register(Subtree())
	Register(HasPath())
	Register(At())
	Register(Nullify())
	Register(JSONPatch())
	Register(KeyedList())
	Register(All())
	Register(If())
	Register(Let())
	Register(Pass())
	Register(Raw())
	Register(Quote())
	Register(Unquote())
	Register(Dive())
	Register(Embed())
	Register(Pipe())
	Register(Let())

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

// Unsafe returns true if the named merge op calls out to the system.
func Unsafe(name string) bool {
	return name == string(pipeName)
}
