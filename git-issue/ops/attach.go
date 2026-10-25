package ops

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/signadot/tony-format/git-issue/issuelib"
)

// AttachmentsDir is where an issue keeps its attachments, under the issue's tree.
const AttachmentsDir = "discussion/files"

// Attach stores files with an issue, each at its path under discussion/files/, and answers the
// issue and the paths stored, sorted. name is what the attachment is called in the commit: the
// file's or the directory's base name. An attachment is a commit on the issue's chain, as a
// comment is, so history keeps what was attached before.
func Attach(s issuelib.Store, id, name string, files map[string]string) (*issuelib.Issue, []string, error) {
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("nothing to attach")
	}
	ref, err := own(s, id)
	if err != nil {
		return nil, nil, err
	}
	issue, _, err := s.GetByRef(ref)
	if err != nil {
		return nil, nil, err
	}
	stored := make(map[string]string, len(files))
	paths := make([]string, 0, len(files))
	for rel, content := range files {
		p, err := attachmentPath(rel)
		if err != nil {
			return nil, nil, err
		}
		stored[p] = content
		paths = append(paths, p)
	}
	sort.Strings(paths)
	msg := fmt.Sprintf("attach: %s (%d file(s))", name, len(stored))
	if err := s.Update(issue, msg, stored); err != nil {
		return nil, nil, fmt.Errorf("failed to attach files: %w", err)
	}
	return issue, paths, nil
}

// attachmentPath is where a file named rel, relative to discussion/files/, is stored. A name
// that is empty, absolute or climbs out of the directory is refused: an attachment lives under
// discussion/files/ and nowhere else in the issue's tree.
func attachmentPath(rel string) (string, error) {
	rel = filepath.ToSlash(rel)
	clean := path.Clean(rel)
	if rel == "" || clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%q is not a name an attachment can have: a relative path, under %s/", rel, AttachmentsDir)
	}
	return path.Join(AttachmentsDir, clean), nil
}

// ReadAttachment reads what `git issue attach <path>` attaches: the file at p, or every file
// under the directory at p, each keyed by its path relative to p's parent, so a directory keeps
// its layout under its own name. It answers the base name too, which names the attachment.
func ReadAttachment(p string) (name string, files map[string]string, err error) {
	info, err := os.Stat(p)
	if err != nil {
		return "", nil, fmt.Errorf("path not found: %s", p)
	}
	name = filepath.Base(p)
	files = map[string]string{}
	if !info.IsDir() {
		content, err := os.ReadFile(p)
		if err != nil {
			return "", nil, fmt.Errorf("failed to read file: %w", err)
		}
		files[name] = string(content)
		return name, files, nil
	}
	err = filepath.WalkDir(p, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		content, err := os.ReadFile(fp)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", fp, err)
		}
		rel, _ := filepath.Rel(p, fp)
		files[filepath.ToSlash(filepath.Join(name, rel))] = string(content)
		return nil
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to walk directory: %w", err)
	}
	return name, files, nil
}
