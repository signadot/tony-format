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

// Extract writes an issue's attachments into dir on this machine, each at its path under
// discussion/files/, and answers the files written, sorted. name picks one attachment, or a
// directory of them, as show lists it or relative to discussion/files/; empty is all of them.
// The bytes are written as stored. Nothing is overwritten: when a destination already holds
// something, that is named and nothing is written. A stored name that would land outside dir
// is refused, since an issue may have come from another clone and attach's check on the way
// in is that clone's.
func Extract(s issuelib.Store, id, name, dir string) ([]string, error) {
	if dir == "" {
		return nil, fmt.Errorf("extract takes a directory to write into")
	}
	ref, err := s.FindRef(id)
	if err != nil {
		return nil, err
	}
	want := path.Clean(filepath.ToSlash(name))
	switch {
	case name == "" || want == AttachmentsDir:
		want = ""
	case strings.HasPrefix(want, AttachmentsDir+"/"):
		want = strings.TrimPrefix(want, AttachmentsDir+"/")
	default:
		want = strings.TrimPrefix(want, "discussion/")
	}
	type extraction struct{ from, to string }
	var picked []extraction
	var comments, attachments []string
	walkDiscussion(s, ref, "discussion", &comments, &attachments)
	for _, p := range attachments {
		rel, ok := strings.CutPrefix(p, AttachmentsDir+"/")
		if !ok {
			rel = strings.TrimPrefix(p, "discussion/")
		}
		if want != "" && rel != want && !strings.HasPrefix(rel, want+"/") {
			continue
		}
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return nil, fmt.Errorf("%s is stored under a name that is not a relative path under %s/, so it has nowhere under %s to go; nothing written", p, AttachmentsDir, dir)
		}
		picked = append(picked, extraction{from: p, to: filepath.Join(dir, filepath.FromSlash(rel))})
	}
	if len(picked) == 0 {
		if want == "" {
			return nil, fmt.Errorf("issue %s has no attachments", id)
		}
		return nil, fmt.Errorf("issue %s has no attachment %s: give one as show lists it", id, name)
	}
	sort.Slice(picked, func(i, j int) bool { return picked[i].to < picked[j].to })
	var there []string
	for _, e := range picked {
		if _, err := os.Lstat(e.to); err == nil {
			there = append(there, e.to)
		}
	}
	if len(there) > 0 {
		return nil, fmt.Errorf("nothing written: already on this machine, and not overwritten: %s", strings.Join(there, ", "))
	}
	written := make([]string, 0, len(picked))
	for _, e := range picked {
		data, err := s.ReadFile(ref, e.from)
		if err != nil {
			return written, fmt.Errorf("failed to read %s: %w", e.from, err)
		}
		if err := os.MkdirAll(filepath.Dir(e.to), 0o755); err != nil {
			return written, err
		}
		// O_EXCL: a file that appeared since the check above is not overwritten either.
		f, err := os.OpenFile(e.to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return written, err
		}
		_, werr := f.Write(data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return written, fmt.Errorf("failed to write %s: %w", e.to, werr)
		}
		written = append(written, e.to)
	}
	return written, nil
}
