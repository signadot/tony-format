// Package dirbuild interprets a tony build directory
package dirbuild

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tony-format/tony"
	"github.com/tony-format/tony/debug"
	"github.com/tony-format/tony/encode"
	"github.com/tony-format/tony/eval"
	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/parse"

	"github.com/goccy/go-yaml"
)

const (
	DefaultSuffix = "-ytool" + ".yaml"
)

type Dir struct {
	Root    string         `json:"-"`
	Suffix  string         `json:"suffix,omitempty"`
	DestDir string         `json:"destDir,omitempty"`
	Sources []DirSource    `json:"sources"`
	Patches []DirPatch     `json:"patches,omitempty"`
	Env     map[string]any `json:"env,omitempty"`

	nameCache map[string]int
}

func OpenDir(path string, env map[string]any) (*Dir, error) {
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

func newDir(yTool *ir.Node, path string, env map[string]any) (*Dir, error) {
	dir := &Dir{
		Root:   path,
		Suffix: DefaultSuffix,
	}
	return initDir(dir, yTool, path, env)
}

func initDir(dir *Dir, yTool *ir.Node, path string, env map[string]any) (*Dir, error) {
	yDir := &ir.Node{}
	if len(yTool.Fields) != 0 {
		if yTool.Fields[0].String == "build" {
			yDir = yTool.Values[0]
		}
	}
	yToolMap := ir.ToMap(yDir)

	yDestDir := yToolMap["destDir"]
	if yDestDir != nil {
		if yDestDir.Type != ir.StringType {
			return nil, fmt.Errorf("destDir should be a string")
		}
		dir.DestDir = yDestDir.String
	}
	if ySuffix := yToolMap["suffix"]; ySuffix != nil {
		if ySuffix.Type != ir.StringType {
			return nil, fmt.Errorf("wrong type for suffix %s (want string)", ySuffix.Type)
		}
		dir.Suffix = ySuffix.String
	}

	yEnv := yToolMap["env"]
	if yEnv != nil {
		if yEnv.Type != ir.ObjectType {
			return nil, fmt.Errorf("wrong type for env %s (want obj)", yEnv.Type)
		}
		tool := tony.DefaultTool()
		oDir, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		if err := os.Chdir(dir.Root); err != nil {
			return nil, err
		}
		defer os.Chdir(oDir)
		yEnv, err := tool.Run(yEnv)
		if err != nil {
			return nil, fmt.Errorf("error evaluating env: %w", err)
		}
		dir.Env = eval.ToJSONAny(yEnv).(map[string]any)

		env, err := mergeEnv(dir.Env, env)
		if err != nil {
			return nil, err
		}
		dir.Env = env
		if debug.LoadEnv() {
			debug.Logf("loaded env %s\n", dir.Env)
		}
	}
	ySources := yToolMap["sources"]
	if ySources != nil {
		if ySources.Type != ir.ArrayType {
			return nil, fmt.Errorf("wrong type for sources: %s", ySources.Type)
		}
		buf := bytes.NewBuffer(nil)
		if err := encode.Encode(ySources, buf); err != nil {
			return nil, fmt.Errorf("error encoding sources for decode: %w", err)
		}
		if err := yaml.Unmarshal(buf.Bytes(), &dir.Sources); err != nil {
			return nil, fmt.Errorf("error decoding sources: %w", err)
		}
	}
	yPatches := yToolMap["patches"]
	if yPatches != nil {
		if yPatches.Type != ir.ArrayType {
			return nil, fmt.Errorf("wrong type for patches: %s", yPatches.Type)
		}
		patches, err := dir.getYPatches(dir.Root, yPatches.Values)
		if err != nil {
			return nil, err
		}
		dir.Patches = patches
		if debug.Patches() {
			for i := range dir.Patches {
				p := &dir.Patches[i]
				debug.Logf("loaded patch %s\n", p)
			}
		}
	}

	dir.nameCache = map[string]int{}
	return dir, nil
}

func mergeEnv(dst, p map[string]any) (map[string]any, error) {
	doc, err := eval.FromJSONAny(dst)
	if err != nil {
		return nil, err
	}
	patch, err := eval.FromJSONAny(p)
	if err != nil {
		return nil, err
	}
	y, err := tony.Patch(doc, patch)
	if err != nil {
		return nil, err
	}
	return eval.ToJSONAny(y).(map[string]any), nil
}
