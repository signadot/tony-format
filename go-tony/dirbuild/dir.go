// Package dirbuild interprets a tony build directory
package dirbuild

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

const (
	DefaultSuffix = "-ytool" + ".yaml"
)

type schema struct{}

type Dir struct {
	schema  `tony:"schemadef=dir"`
	Root    string              `json:"-"`
	Suffix  string              `json:"suffix,omitempty"`
	DestDir string              `json:"destDir,omitempty"`
	Sources []DirSource         `json:"sources"`
	Patches []DirPatch          `json:"patches,omitempty"`
	Env     map[string]*ir.Node `json:"env,omitempty"`

	nameCache map[string]int
}

func OpenDir(path string, env map[string]*ir.Node) (*Dir, error) {
	if debug.LoadEnv() {
		debug.Logf("OpenDir input env:\n%s", debug.JSON(env))
	}
	// Try build.{tony,yaml,json} in order
	extensions := []string{".tony", ".yaml", ".json"}
	var tyPath string
	var d []byte
	var found bool

	for _, ext := range extensions {
		candidatePath := filepath.Join(path, "build"+ext)
		var err error
		d, err = os.ReadFile(candidatePath)
		if err == nil {
			// File found, use it
			tyPath = candidatePath
			found = true
			break
		}
		if !os.IsNotExist(err) {
			// File exists but couldn't be read (permissions, etc.)
			return nil, fmt.Errorf("could not read %q: %w", candidatePath, err)
		}
		// File doesn't exist, try next extension
	}
	if !found {
		return nil, fmt.Errorf("could not find build.{tony,yaml,json} in %q", path)
	}
	y, err := parse.Parse(d)
	if err != nil {
		return nil, fmt.Errorf("could not decode %s: %w", tyPath, err)
	}
	return newDir(y, path, env)
}

func newDir(node *ir.Node, path string, env map[string]*ir.Node) (*Dir, error) {

	dir := &Dir{
		Root:   path,
		Suffix: DefaultSuffix,
	}
	return initDir(dir, node, path, env)
}

func initDir(dir *Dir, node *ir.Node, path string, env map[string]*ir.Node) (*Dir, error) {
	oDir := &ir.Node{}
	if len(node.Fields) != 0 {
		if node.Fields[0].String == "build" {
			oDir = node.Values[0]
		}
	}
	if err := gomap.FromIR(oDir, dir); err != nil {
		return nil, err
	}
	if dir.Suffix == "" {
		dir.Suffix = DefaultSuffix
	}

	if dir.Env != nil {
		tool := tony.DefaultTool()
		orgDir, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		if err := os.Chdir(dir.Root); err != nil {
			return nil, err
		}
		defer os.Chdir(orgDir)
		evaldEnv, err := tool.Run(ir.FromMap(dir.Env))
		if err != nil {
			return nil, fmt.Errorf("error evaluating env: %w", err)
		}
		pEnv, err := tony.Patch(evaldEnv, ir.FromMap(env))
		if err != nil {
			return nil, err
		}
		dir.Env = ir.ToMap(pEnv)
		if debug.LoadEnv() {
			debug.Logf("loaded env %v\n", dir.Env)
		}
	}
	if err := dir.filterPatches(); err != nil {
		return nil, err
	}
	dir.nameCache = map[string]int{}
	return dir, nil
}

func (dir *Dir) filterPatches() error {
	j := 0
	for i := range dir.Patches {
		dp := &dir.Patches[i]
		if dp.If == "" {
			dir.Patches[j] = *dp
			j++
			continue
		}
		m, err := eval.ExpandIR(ir.FromString(dp.If), toEvalEnv(dir.Env))
		if err != nil {
			return err
		}
		if m.Type == ir.BoolType && !m.Bool {
			continue
		}
		dir.Patches[j] = *dp
		j++
	}
	return nil
}

func toEvalEnv(m map[string]*ir.Node) eval.Env {
	res := make(eval.Env, len(m))
	for k, v := range m {
		res[k] = v
	}
	return res
}
