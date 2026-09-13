package main

import (
	"context"
	"testing"

	"go.lsp.dev/protocol"
)

func change(sl, sc, el, ec uint32, text string) protocol.TextDocumentContentChangeEvent {
	return protocol.TextDocumentContentChangeEvent{
		Range: protocol.Range{
			Start: protocol.Position{Line: sl, Character: sc},
			End:   protocol.Position{Line: el, Character: ec},
		},
		Text: text,
	}
}

// The server's copy of a document follows the editor's through incremental
// changes, whose positions are lines and UTF-16 columns. An insert at the
// document's start replaced the document, an insert at its end was dropped, and a
// byte offset was used as a rune index, so an edit after a multi-byte character
// landed in the wrong place; formatting then wrote the corrupted copy back over
// the editor's (4ynqp7wqh12krg32msn0 item 28).
func TestIncrementalSyncFollowsTheEditor(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
		changes             []protocol.TextDocumentContentChangeEvent
	}{
		{"insert at the start", "a: 1\nb: 2\n", "# hdr\na: 1\nb: 2\n",
			[]protocol.TextDocumentContentChangeEvent{change(0, 0, 0, 0, "# hdr\n")}},
		{"insert at the end", "a: 1\n", "a: 1\nb: 2\n",
			[]protocol.TextDocumentContentChangeEvent{change(1, 0, 1, 0, "b: 2\n")}},
		{"replace after a two-byte rune", "é: 1\n", "é: 2\n",
			[]protocol.TextDocumentContentChangeEvent{change(0, 3, 0, 4, "2")}},
		{"replace after a surrogate pair", "😀: 1\n", "😀: 2\n",
			[]protocol.TextDocumentContentChangeEvent{change(0, 4, 0, 5, "2")}},
		{"replace across lines", "a: 1\nb: 2\nc: 3\n", "a: 1\nx: 9\nc: 3\n",
			[]protocol.TextDocumentContentChangeEvent{change(1, 0, 2, 0, "x: 9\n")}},
		{"delete a line", "a: 1\nb: 2\n", "a: 1\n",
			[]protocol.TextDocumentContentChangeEvent{change(1, 0, 2, 0, "")}},
		{"two changes in one notification", "a: 1\n", "a: 1\nb: 2\nc: 3\n",
			[]protocol.TextDocumentContentChangeEvent{change(1, 0, 1, 0, "b: 2\n"), change(2, 0, 2, 0, "c: 3\n")}},
		{"a column past the line's end is the line's end", "a: 1\n", "a: 1 # c\n",
			[]protocol.TextDocumentContentChangeEvent{change(0, 4, 0, 99, " # c")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{docs: &documentStore{docs: map[string]*document{}}}
			s.docs.put("file:///t.tony", tc.before, 1)
			err := s.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
				TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: "file:///t.tony"}, Version: 2},
				ContentChanges: tc.changes,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := s.docs.get("file:///t.tony").content; got != tc.after {
				t.Errorf("server holds %q, want %q", got, tc.after)
			}
		})
	}
}

// One column, three units: the protocol's UTF-16, the tokenizer's bytes, and the
// token search's runes.
func TestColumnConversions(t *testing.T) {
	const line = "😀é: x" // 😀 is 4 bytes, 2 UTF-16 units; é is 2 bytes, 1 unit
	if got := byteCol(line, 3); got != 6 {
		t.Errorf("byteCol(3) = %d, want 6", got)
	}
	if got := runeCol(line, 3); got != 2 {
		t.Errorf("runeCol(3) = %d, want 2", got)
	}
	if got := runeIndex(line, 6); got != 2 {
		t.Errorf("runeIndex(6) = %d, want 2", got)
	}
	if got := utf16Col(line, 2); got != 3 {
		t.Errorf("utf16Col(2) = %d, want 3", got)
	}
	if got := utf16Col(line, 99); got != 6 {
		t.Errorf("utf16Col past the end = %d, want the line's 6", got)
	}
	if got := byteOffset("a\nbc\n", 2, 0); got != 5 {
		t.Errorf("byteOffset past the last line = %d, want the document's end 5", got)
	}
}
