package dirbuild

import (
	"fmt"
	"strings"
)

// DirOutput consolidates output configuration for a build directory.
// It holds the destination directory, file suffix, filename strategy,
// and optional k8s-specific filename configuration.
//
//tony:schemagen=diroutput
type DirOutput struct {
	DestDir   string        `tony:"field=destDir"`
	Suffix    string        `tony:"field=suffix"`
	Filenames string        `tony:"field=filenames"` // "auto" or future values
	K8s       *DirOutputK8s `tony:"field=k8s"`
}

// DirOutputK8s holds Kubernetes-specific output configuration.
//
//tony:schemagen=diroutputk8s
type DirOutputK8s struct {
	Filenames string `tony:"field=filenames"` // dash-separated tokens: name, kind, namespace
}

// validK8sTokens is the set of allowed tokens in a k8s.filenames pattern.
var validK8sTokens = map[string]bool{
	"name":      true,
	"kind":      true,
	"namespace": true,
}

// validate checks that configured filename patterns use only valid tokens.
func (o *DirOutput) validate() error {
	if o.K8s != nil && o.K8s.Filenames != "" {
		tokens := strings.Split(o.K8s.Filenames, "-")
		for _, tok := range tokens {
			if !validK8sTokens[tok] {
				return fmt.Errorf("invalid k8s.filenames token %q (valid: name, kind, namespace)", tok)
			}
		}
	}
	return nil
}
