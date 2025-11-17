package dirbuild

import (
	"fmt"
	"os"

	"github.com/signadot/tony-format/tony"
	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/encode"
	"github.com/signadot/tony-format/tony/eval"
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/parse"
)

func (d *Dir) patch(dst []*ir.Node) error {
	for i, doc := range dst {
		for j := range d.Patches {
			if doc == nil {
				break
			}
			dirPatch := &d.Patches[j]
			match, err := tony.Match(doc, dirPatch.Match)
			if err != nil {
				return fmt.Errorf("match error: %w", err)
			}
			if debug.Patch() {
				debug.Logf("# doc\n%s\n---\n# dirpath\n%s\n---\n# matched\n%t\n",
					encode.MustString(doc), encode.MustString(dirPatch.Match), match)
			}
			if !match {
				continue
			}
			out, err := tony.Patch(doc, dirPatch.Patch)
			if err != nil {
				return fmt.Errorf("error patching patch %d doc %d: %w", j, i, err)
			}
			if out != nil {
				if debug.Patch() {
					debug.Logf("patched\n%s\n", encode.MustString(out))
				}
				dst[i] = out.Clone()
			} else {
				if debug.Patch() {
					debug.Logf("patch deleted result\n")
				}
				dst[i] = nil
			}
			doc = out
		}
	}
	return nil
}

type DirPatch struct {
	Match *ir.Node `json:"match,omitempty" yaml:"match,omitempty"`
	Patch *ir.Node `json:"patch,omitempty" yaml:"patch,omitempty"`
	File  string   `json:"file,omitempty" yaml:"file,omitempty"`
	If    string   `json:"if,omitempty" yaml:"if,omitempty"`
}

func (d *DirPatch) String() string {
	return fmt.Sprintf("match: %s patch: %s file: %s if: %s",
		encode.MustString(d.Match), encode.MustString(d.Patch), d.File, d.If)
}

func (d *Dir) getFilePatches(root, file string) ([]DirPatch, error) {
	fileBytes, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	fy, err := parse.Parse(fileBytes)
	if err != nil {
		return nil, fmt.Errorf("error decoding %s: %w", file, err)
	}
	if fy.Type != ir.ArrayType {
		return nil, fmt.Errorf("wrong type for file %s of patches, expected list", file)
	}
	return d.getYPatches(root, fy.Values)
}

func (d *Dir) getYPatches(root string, ys []*ir.Node) ([]DirPatch, error) {
	res := []DirPatch{}
	for i, ydPatch := range ys {
		match := ir.Get(ydPatch, "match")
		patch := ir.Get(ydPatch, "patch")
		file := ir.Get(ydPatch, "file")
		cond := ir.Get(ydPatch, "if")
		if cond != nil {
			if cond.Type != ir.StringType {
				return nil, fmt.Errorf("invalid patch if, expected string")
			}
			if err := eval.ExpandEnv(cond, d.Env); err != nil {
				return nil, fmt.Errorf("error expanding if %q: %w", cond.String, err)
			}
			if !ir.Truth(cond) {
				continue
			}
		}
		if file == nil {
			if match == nil {
				return nil, fmt.Errorf("no match in patch %d", i)
			}
			if patch == nil {
				return nil, fmt.Errorf("no patch yaml in patch %d", i)
			}
			if err := eval.ExpandEnv(match, d.Env); err != nil {
				return nil, fmt.Errorf("error expanding match: %w", err)
			}
			if err := eval.ExpandEnv(patch, d.Env); err != nil {
				return nil, fmt.Errorf("error expanding patch: %w", err)
			}
			res = append(res, DirPatch{Match: match, Patch: patch})
			continue
		}
		if file.Type != ir.StringType {
			return nil, fmt.Errorf("patch %d: file: expected string (path)", i)
		}
		filePatches, err := d.getFilePatches(root, file.String)
		if err != nil {
			return nil, fmt.Errorf("could not get patches from %s: %w", file.String, err)
		}
		res = append(res, filePatches...)
	}
	return res, nil
}
