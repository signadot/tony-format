package schema

type Context map[string]map[string]bool

const TonyFormatContextURI = "tony-format.org/schema/contexts"

func DefaultContext() Context {
	return Context(map[string]map[string]bool{
		"tony-format.org/schema/contexts": {
			"encoding": true,
			"eval":     true,
			"match":    true,
			"patch":    true,
			"diff":     true,
			"schema":   true,
		},
	})
}
