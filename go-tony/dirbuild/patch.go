package dirbuild

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
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
			if debug.Patches() {
				debug.Logf("# patch IR being applied:\n%s\n", encode.MustString(dirPatch.Patch))
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

// DirPatch represents a transformation to apply to matching documents.
// Either Match+Patch should be set (inline patch) or File should be set
// (load patches from external file).
type DirPatch struct {
	schema `tony:"schemagen=dirpatch"`
	Match  *ir.Node `tony:"field=match"` // Pattern to match documents against
	Patch  *ir.Node `tony:"field=patch"` // Transformation to apply to matched documents
	File   string   `tony:"field=file"`  // External file containing patch definitions
	If     string   `tony:"field=if"`    // Conditional expression; patch is skipped if false
}

func (d *DirPatch) String() string {
	return fmt.Sprintf("match: %s patch: %s file: %s if: %s",
		encode.MustString(d.Match), encode.MustString(d.Patch), d.File, d.If)
}
