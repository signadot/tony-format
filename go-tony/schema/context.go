package schema

type Context struct {
	OutIn map[string]map[string]bool
	InOut map[string]string
}

const TonyFormatURI = "tony-format/schema"

func DefaultContext() *Context {
	return &Context{
		OutIn: map[string]map[string]bool{
			TonyFormatURI: {
				"encoding": true,
				"eval":     true,
				"match":    true,
				"patch":    true,
				"diff":     true,
				"schema":   true,
			},
		},
		InOut: map[string]string{
			"encoding": TonyFormatURI,
			"eval":     TonyFormatURI,
			"match":    TonyFormatURI,
			"patch":    TonyFormatURI,
			"diff":     TonyFormatURI,
			"schema":   TonyFormatURI,
		},
	}
}
