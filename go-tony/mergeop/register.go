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

// NamespaceSep divides a namespace from an operation name: acme:shout.
//
// It is ':' because the alternatives are taken or unavailable. '.' composes tags, so
// !acme.shout is two operations rather than one namespaced one; YAML's verbatim
// !<tag:example.com,2026:shout> does not parse here, its comma ending the tag. A tag name
// otherwise runs to whitespace, so ':' is free, and it is the separator YAML itself uses
// for namespaced tag handles.
const NamespaceSep = ":"

// Register adds a built-in operation, under a name with no namespace.
//
// The names without a namespace are reserved for this package. That is not a courtesy: a
// consumer who registers !policy today, and upgrades into a release where !policy is
// built in, gets ErrSymbolExists from an init that probably ignores it -- and their
// operation silently stops existing, in documents already written. Their tag has to be
// somewhere this package will never build.
//
// So a consumer calls RegisterNamespaced, and Register refuses a namespaced name to keep
// the two sets from meeting in the middle.
func Register(s Symbol) error {
	if strings.Contains(s.String(), NamespaceSep) {
		return fmt.Errorf("symbol %q is namespaced: use RegisterNamespaced", s.String())
	}
	return register(s, s.String())
}

// RegisterNamespaced adds an operation under a namespace of the caller's choosing: the
// tag is written !<namespace>:<name>, and no release of this package will ever take that
// name, because Register cannot spell one.
//
// The namespace should be something a consumer owns -- an organisation, a product -- for
// the same reason two of them must not choose "ext".
func RegisterNamespaced(namespace string, s Symbol) error {
	if err := validNamespace(namespace); err != nil {
		return err
	}
	if strings.Contains(s.String(), NamespaceSep) {
		return fmt.Errorf("symbol %q is already namespaced", s.String())
	}
	return register(namespaced{Symbol: s, name: namespace + NamespaceSep + s.String()},
		namespace+NamespaceSep+s.String())
}

func register(s Symbol, key string) error {
	if strings.Contains(key, ".") {
		return fmt.Errorf("symbol %q must not contain '.'", key)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, present := d[key]; present {
		return fmt.Errorf("%s: %w", key, ErrSymbolExists)
	}
	d[key] = s
	return nil
}

// validNamespace holds a namespace to what a tag name can hold and a reader can tell
// apart from an operation: no separator of its own, and nothing that ends a tag.
func validNamespace(ns string) error {
	switch {
	case ns == "":
		return fmt.Errorf("namespace must not be empty")
	case strings.ContainsAny(ns, ".:()"):
		return fmt.Errorf("namespace %q must not contain any of '.:()'", ns)
	case strings.ContainsAny(ns, " \t\n"):
		return fmt.Errorf("namespace %q must not contain whitespace", ns)
	}
	return nil
}

// namespaced is a Symbol answering to its namespaced name. Everything else about it --
// what it instantiates, whether it matches or patches -- is the Symbol it wraps.
type namespaced struct {
	Symbol
	name string
}

func (n namespaced) String() string { return n.name }

// Summary delegates, because namespaced embeds the Symbol INTERFACE: the wrapped type's
// own methods are not promoted through it, so a Symbol which describes itself would stop
// doing so the moment it was namespaced.
func (n namespaced) Summary() string {
	if s, ok := n.Symbol.(Summarized); ok {
		return s.Summary()
	}
	return ""
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
	Register(IR())
	Register(Subtree())
	Register(HasPath())
	Register(At())
	Register(GetPath())
	Register(ListPath())
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

	// comments, which are neither data nor tags and needed an absolute operator
	// of their own: without one, changing a comment meant replacing the value it
	// describes.
	Register(Comment())
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
