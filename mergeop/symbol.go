package mergeop

import "github.com/tony-format/tony/ir"

type Symbol interface {
	Name
	Instance(child *ir.Node, args []string) (Op, error)
}

type Name interface {
	String() string
	IsMatch() bool
	IsPatch() bool
}

type name string

func (s name) String() string {
	return string(s)
}
func (s name) IsMatch() bool { return true }
func (s name) IsPatch() bool { return true }

type matchName string

func (s matchName) String() string {
	return string(s)
}
func (s matchName) IsMatch() bool {
	return true
}
func (s matchName) IsPatch() bool {
	return false
}

type patchName string

func (s patchName) String() string {
	return string(s)
}
func (s patchName) IsMatch() bool {
	return false
}
func (s patchName) IsPatch() bool {
	return true
}
