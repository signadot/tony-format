package snap

import (
	"io"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/stream"
)

// TestChunkNeverSplitsAValueFromItsHeadComment: a chunk began at a VALUE start,
// and a head comment precedes its value -- so the comment's bytes fell before the
// index offset of the value it describes. A read seeking to that offset cannot
// reach them at all: they are on the far side of the seek, not outside a window
// that could be widened. The builder holds a head comment the way it always held
// a key, so the chunk begins at the comment (3cdjz00jh12krns4g1n0).
func TestChunkNeverSplitsAValueFromItsHeadComment(t *testing.T) {
	for _, size := range []string{"1", "16", "64"} {
		t.Run("chunk size "+size, func(t *testing.T) {
			t.Setenv("SNAP_MAX_CHUNK_SIZE", size)
			s, index := snapOf(t, commentDoc)
			evs, offs := eventOffsets(t, s)

			boundary := map[int64]string{}
			for _, e := range index.Entries {
				boundary[e.Offset] = e.Path.String()
			}
			comments := 0
			for i, ev := range evs {
				if ev.Type != stream.EventHeadComment {
					continue
				}
				comments++
				// find the value start this comment introduces
				for j := i + 1; j < len(evs); j++ {
					if !evs[j].IsValueStart() {
						continue
					}
					for off := offs[i] + 1; off <= offs[j]; off++ {
						if p, ok := boundary[off]; ok {
							t.Errorf("a chunk for %q begins at %d, between the comment %v at %d "+
								"and the value it describes at %d", p, off, ev.CommentLines, offs[i], offs[j])
						}
					}
					break
				}
			}
			if comments == 0 {
				t.Fatal("no head comments in the stream, so this proves nothing")
			}
		})
	}
}

// TestReadPathKeepsComments is the rule section 5 builds to: a path read answers
// with what the path names in the document, comments and all.
func TestReadPathKeepsComments(t *testing.T) {
	doc, err := parse.Parse([]byte(commentDoc), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"", "name", "spec", "spec.replicas", "spec.items", "spec.items[0]",
		"spec.items[0].id", "spec.items[1]", "ports", "ports[0]"}
	for _, size := range []string{"1", "64", "4096"} {
		t.Run("chunk size "+size, func(t *testing.T) {
			t.Setenv("SNAP_MAX_CHUNK_SIZE", size)
			s, _ := snapOf(t, commentDoc)
			for _, p := range paths {
				want, err := doc.GetKPathWith(p, ir.WithComments(true))
				if err != nil {
					t.Fatalf("GetKPath(%q): %v", p, err)
				}
				got, err := s.ReadPath(p)
				if err != nil {
					t.Fatalf("ReadPath(%q): %v", p, err)
				}
				if !got.DeepEqualWithComments(want) {
					t.Errorf("read %q as\n%s\nand the document has\n%s", p, show(t, got), show(t, want))
				}
			}
		})
	}
}

// TestBothReadersAgree: ReadPath materializes and ReadPathEventReader streams,
// and the streaming one is what replays a whole snapshot as the base of a read.
// They are two copies of one window, so they are checked against each other
// rather than trusted to have been edited together.
func TestBothReadersAgree(t *testing.T) {
	paths := []string{"", "name", "spec", "spec.replicas", "spec.items", "spec.items[0]",
		"spec.items[0].id", "spec.items[1]", "ports", "ports[0]"}
	for _, size := range []string{"1", "64", "4096"} {
		t.Run("chunk size "+size, func(t *testing.T) {
			t.Setenv("SNAP_MAX_CHUNK_SIZE", size)
			s, _ := snapOf(t, commentDoc)
			for _, p := range paths {
				want, err := s.ReadPath(p)
				if err != nil {
					t.Fatalf("ReadPath(%q): %v", p, err)
				}
				rd, err := s.ReadPathEventReader(p)
				if err != nil {
					t.Fatalf("ReadPathEventReader(%q): %v", p, err)
				}
				var evs []stream.Event
				for {
					ev, err := rd.ReadEvent()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatalf("ReadEvent(%q): %v", p, err)
					}
					evs = append(evs, *ev)
				}
				got, err := stream.EventsToNode(evs)
				if err != nil {
					t.Fatalf("EventsToNode(%q): %v", p, err)
				}
				if !got.DeepEqualWithComments(want) {
					t.Errorf("streaming %q gave\n%s\nand ReadPath gave\n%s", p, show(t, got), show(t, want))
				}
			}
		})
	}
}
