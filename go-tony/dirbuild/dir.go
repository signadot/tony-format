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
	schema  `tony:"schemagen=dir"`
	Root    string              `tony:"omit"`
	Suffix  string              `tony:"field=suffix"`
	DestDir string              `tony:"field=destDir"`
	Sources []DirSource         `tony:"field=sources"`
	Patches []DirPatch          `tony:"field=patches"`
	Env     map[string]*ir.Node `tony:"field=env"`

	nameCache map[string]int
}

func OpenDir(path string, env map[string]*ir.Node) (*Dir, error) {
	if debug.LoadEnv() {
		debug.Logf("OpenDir input env:\n%s", debug.Tony{ir.FromMap(env)})
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
	node, err := parse.Parse(d, parse.ParseComments(true))
	if err != nil {
		return nil, fmt.Errorf("could not decode %s: %w", tyPath, err)
	}
	return newDir(node, path, env)
}

func newDir(node *ir.Node, path string, env map[string]*ir.Node) (*Dir, error) {

	dir := &Dir{
		Root:   path,
		Suffix: DefaultSuffix,
	}
	return initDir(dir, node, path, env)
}

func initDir(dir *Dir, node *ir.Node, path string, env map[string]*ir.Node) (*Dir, error) {
	if node.Type == ir.CommentType {
		node = node.Values[0]
	}
	oDir := &ir.Node{}
	if len(node.Fields) != 0 {
		if node.Fields[0].String == "build" {
			oDir = node.Values[0]
		}
	}
	if oDir.Type == ir.CommentType {
		oDir = oDir.Values[0]
	}
	if err := dir.FromTonyIR(oDir); err != nil {
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
			debug.Logf("loaded env %s\n", debug.Tony{ir.FromMap(dir.Env)})
		}
	}

	evalEnv := eval.EnvToMapAny(dir.Env)
	ps, err := dir.loadPatches(dir.Patches, evalEnv)
	if err != nil {
		return nil, err
	}
	dir.Patches = ps
	dir.nameCache = map[string]int{}
	return dir, nil
}

func (dir *Dir) loadPatches(ps []DirPatch, ee map[string]any) ([]DirPatch, error) {
	res := []DirPatch{}
	for i := range ps {
		p := &ps[i]
		m, err := eval.ExpandIR(ir.FromString(p.If), ee)
		if err != nil {
			return nil, err
		}
		if m.Type == ir.BoolType && !m.Bool {
			continue
		}
		if p.File != "" {
			dps := []DirPatch{}
			d, err := os.ReadFile(p.File)
			if err != nil {
				return nil, err
			}
			if err := gomap.FromTony(d, &dps); err != nil {
				return nil, err
			}
			loaded, err := dir.loadPatches(dps, ee)
			if err != nil {
				return nil, err
			}
			res = append(res, loaded...)
			continue
		}
		if p.Match == nil {
			return nil, fmt.Errorf("missing match: field")
		}
		if p.Patch == nil {
			return nil, fmt.Errorf("missing patch: field")
		}
		p.Match, err = eval.ExpandIR(p.Match, ee)
		if err != nil {
			return nil, fmt.Errorf("error expanding match: %w", err)
		}
		p.Patch, err = eval.ExpandIR(p.Patch, ee)
		if err != nil {
			return nil, fmt.Errorf("error expanding patch: %w", err)
		}
		res = append(res, DirPatch{Match: p.Match, Patch: p.Patch})
	}
	return res, nil
}
