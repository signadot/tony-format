package main

import "github.com/signadot/tony-format/go-tony/format"

type schema struct{}

type Dir struct {
	schema  `tony:"schemagen=dir"`
	Root    string              `json:"-" tony:"omit"`
	Suffix  string              `json:"suffix,omitempty"`
	DestDir string              `json:"destDir,omitempty"`
	Sources []DirSource         `json:"sources"`
	Env     map[string]*ir.Node `json:"env,omitempty"`
}

// DirSource represents a data source for a dirbuild.
type DirSource struct {
	schema `tony:"schemagen=dirsource"`
	Format *format.Format `json:"format,omitempty"`
	Exec   *string        `json:"exec,omitempty"`
	Dir    *string        `json:"dir,omitempty"`
	URL    *string        `json:"url,omitempty"`
}
