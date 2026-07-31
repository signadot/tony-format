package gomap

import (
	"strings"
	"testing"
)

type strictTarget struct {
	Name  string `tony:"field=name"`
	Count int    `tony:"field=count"`
}

type strictOuter struct {
	Inner strictTarget `tony:"field=inner"`
}

// The default stays lenient: a decoder must be able to read a document written by a
// newer peer, ignoring fields it does not know yet.
func TestStrict_LenientByDefault(t *testing.T) {
	var got strictTarget
	if err := FromTony([]byte(`{name: "a", count: 1, futureField: true}`), &got); err != nil {
		t.Fatalf("default decode rejected an unknown field: %v", err)
	}
	if got.Name != "a" || got.Count != 1 {
		t.Errorf("known fields not decoded: %+v", got)
	}
}

func TestStrict_RejectsUnknownField(t *testing.T) {
	var got strictTarget
	err := FromTony([]byte(`{name: "a", count: 1, typoed: true}`), &got, Strict())
	if err == nil {
		t.Fatal("Strict() accepted an unknown field")
	}
	if !strings.Contains(err.Error(), "typoed") {
		t.Errorf("error does not name the offending field: %v", err)
	}
}

func TestStrict_AcceptsExactlyTheDeclaredFields(t *testing.T) {
	var got strictTarget
	if err := FromTony([]byte(`{name: "a", count: 1}`), &got, Strict()); err != nil {
		t.Fatalf("Strict() rejected a document of only declared fields: %v", err)
	}
	if got.Name != "a" || got.Count != 1 {
		t.Errorf("decoded %+v", got)
	}
}

// A partial document is still fine — Strict is about fields that are present and
// unknown, not about fields that are absent.
func TestStrict_AllowsMissingFields(t *testing.T) {
	var got strictTarget
	if err := FromTony([]byte(`{name: "a"}`), &got, Strict()); err != nil {
		t.Fatalf("Strict() rejected a partial document: %v", err)
	}
	if got.Name != "a" || got.Count != 0 {
		t.Errorf("decoded %+v", got)
	}
}

// Strictness has to reach nested values, not just the top level.
func TestStrict_RejectsUnknownFieldNested(t *testing.T) {
	var got strictOuter
	err := FromTony([]byte(`{inner: {name: "a", nope: 1}}`), &got, Strict())
	if err == nil {
		t.Fatal("Strict() accepted an unknown field inside a nested struct")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error does not name the offending field: %v", err)
	}
}

// Renaming via field= must not make the Go field name look unknown: Strict rejects
// fields the type does not declare, it does not tighten how declared ones are matched.
func TestStrict_StillAcceptsGoFieldName(t *testing.T) {
	var got strictTarget
	if err := FromTony([]byte(`{Name: "a"}`), &got, Strict()); err != nil {
		t.Fatalf("Strict() rejected a field under its Go name: %v", err)
	}
	if got.Name != "a" {
		t.Errorf("decoded %+v", got)
	}
}

func TestIsStrict(t *testing.T) {
	if IsStrict() {
		t.Error("IsStrict() with no options = true, want false")
	}
	if IsStrict(ParseTony()) {
		t.Error("IsStrict() with an unrelated option = true, want false")
	}
	if !IsStrict(Strict()) {
		t.Error("IsStrict(Strict()) = false, want true")
	}
	if !IsStrict(ParseTony(), Strict(), UnmapComments(false)) {
		t.Error("IsStrict() did not see Strict() among other options")
	}
}
