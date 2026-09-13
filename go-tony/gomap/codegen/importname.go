package codegen

import (
	"go/build"
	"strings"
	"sync"
)

// pkgNames caches what pkgNameOf answers, by import path.
var pkgNames sync.Map

// pkgNameOf is the package name declared by the package at importPath: what a
// source file refers to it by when the import is not aliased. It was taken to be
// the last element of the path, which it is not for example.com/lib-go (package
// lib), gopkg.in/yaml.v3 (yaml) or a path ending in /v2, so a type from such a
// package was written `lib-go.T` in generated code and left unresolved in the
// parser (p478tacqh12krg32msn0 item 25). A package go/build cannot load keeps
// the path's last element, which is what an unresolvable path had before.
func pkgNameOf(importPath string) string {
	if name, ok := pkgNames.Load(importPath); ok {
		return name.(string)
	}
	name := importPath[strings.LastIndex(importPath, "/")+1:]
	if pkg, err := build.Import(importPath, "", 0); err == nil && pkg.Name != "" {
		name = pkg.Name
	}
	pkgNames.Store(importPath, name)
	return name
}
