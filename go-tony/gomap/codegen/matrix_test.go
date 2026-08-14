package codegen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/matrix"
	"github.com/signadot/tony-format/go-tony/parse"
)

// The matrix tests hold codegen to the requirement it kept being claimed to
// meet: every composition of pointer, list and map, over a scalar and over a
// struct with its own codec, to three levels.
//
// They round-trip VALUES -- struct to IR, IR to text, text back to IR, back to
// struct -- and compare with reflect.DeepEqual. The tests this replaces asserted
// that generated code contained a substring, which passes as soon as one level
// emits something plausible and cannot fail for a shape nobody wrote down. That
// is how "supports container types recursively" was claimed while [][]string did
// not generate at all.

func fullM() *matrix.M {
	ptrSl := []string{"a", "b"}
	ptrMp := map[string]string{"k": "v"}
	ptrSlMp := []map[string]string{{"a": "1"}, {"b": "2"}}
	ptrMpSl := map[string][]string{"k": {"x", "y"}}
	inner := []string{"deep"}
	ptrSlPtr := []*matrix.Leaf{{Name: "one"}, {Name: "two"}}
	str := "pp"
	pstr := &str
	leaf := &matrix.Leaf{Name: "ppLeaf"}
	ptrAr := [2]string{"p", "q"}
	arSlot := []string{"slot"}

	return &matrix.M{
		Sl:    []string{"a", "b"},
		Mp:    map[string]string{"k": "v"},
		PtrSl: &ptrSl,
		PtrLf: &matrix.Leaf{Name: "leaf"},
		SlLf:  []matrix.Leaf{{Name: "a"}, {Name: "b"}},
		MpLf:  map[string]matrix.Leaf{"k": {Name: "v"}},

		SlSl:   [][]string{{"a", "b"}, {}, {"c"}},
		SlMp:   []map[string]string{{"a": "1"}, {"b": "2"}},
		MpSl:   map[string][]string{"k": {"x", "y"}, "empty": {}},
		MpMp:   map[string]map[string]string{"outer": {"inner": "v"}},
		PtrMp:  &ptrMp,
		SlPtr:  []*matrix.Leaf{{Name: "one"}, {Name: "two"}},
		MpPtr:  map[string]*matrix.Leaf{"k": {Name: "v"}},
		SlSlLf: [][]matrix.Leaf{{{Name: "a"}}, {{Name: "b"}, {Name: "c"}}},
		MpSlLf: map[string][]matrix.Leaf{"k": {{Name: "v"}}},

		SlSlSl:   [][][]string{{{"deep"}}, {{"a", "b"}, {"c"}}},
		SlMpSl:   []map[string][]string{{"k": {"a", "b"}}},
		MpSlMp:   map[string][]map[string]string{"k": {{"a": "1"}, {"b": "2"}}},
		PtrSlMp:  &ptrSlMp,
		PtrMpSl:  &ptrMpSl,
		SlPtrSl:  []*[]string{&inner},
		MpPtrSl:  map[string]*[]string{"k": &inner},
		MpSlPtr:  map[string][]*matrix.Leaf{"k": {{Name: "v"}}},
		PtrSlPtr: &ptrSlPtr,

		PP:   &pstr,
		PPLf: &leaf,

		Ar:      [2]string{"a", "b"},
		SlAr:    [][2]string{{"c", "d"}, {"e", "f"}},
		ArSl:    [2][]string{{"g"}, {"h", "i"}},
		PtrAr:   &ptrAr,
		ArLeaf:  [2]matrix.Leaf{{Name: "one"}, {Name: "two"}},
		ArPtrSl: [2]*[]string{&arSlot, nil},

		MpKey:   map[matrix.Key]string{"k": "v"},
		SlMpKey: []map[matrix.Key]string{{"a": "1"}},
		MpKeySl: map[matrix.Key][]string{"k": {"x", "y"}},
	}
}

// roundTripM takes a value the whole way a document goes, because a codec that
// only agrees with itself in memory has not been tested against anything.
func roundTripM(t *testing.T, in *matrix.M) *matrix.M {
	t.Helper()
	node, err := in.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	text := encode.MustString(node)
	parsed, err := parse.Parse([]byte(text))
	if err != nil {
		t.Fatalf("parse of\n%s\n: %v", text, err)
	}
	out := &matrix.M{}
	if err := out.FromTonyIR(parsed); err != nil {
		t.Fatalf("FromTonyIR of\n%s\n: %v", text, err)
	}
	return out
}

// TestMatrix_EveryCompositionRoundTrips checks each field on its own, so a
// failure names the composition that broke rather than "the struct differs".
func TestMatrix_EveryCompositionRoundTrips(t *testing.T) {
	in := fullM()
	out := roundTripM(t, in)

	inV, outV := reflect.ValueOf(*in), reflect.ValueOf(*out)
	typ := inV.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		t.Run(f.Name+" "+f.Type.String(), func(t *testing.T) {
			want, got := inV.Field(i).Interface(), outV.Field(i).Interface()
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("%s round-tripped to\n\t%#v\nwant\n\t%#v", f.Type, got, want)
			}
		})
	}
}

// TestMatrix_EmptyAndNilAreDistinct: at every depth, an empty container has to
// come back empty and a nil one nil. This is the property the whole matrix
// exists for -- a codec that turns [] into nil loses a statement the document
// made.
func TestMatrix_EmptyAndNilAreDistinct(t *testing.T) {
	emptySl := []string{}
	emptyMp := map[string]string{}

	in := &matrix.M{
		SlSl:    [][]string{{}},               // a list holding an empty list
		MpSl:    map[string][]string{"k": {}}, // a map holding an empty list
		MpMp:    map[string]map[string]string{"k": {}},
		PtrSl:   &emptySl,
		PtrMp:   &emptyMp,
		SlPtrSl: []*[]string{&emptySl},
	}
	out := roundTripM(t, in)

	if len(out.SlSl) != 1 || out.SlSl[0] == nil || len(out.SlSl[0]) != 0 {
		t.Errorf("[][]string{{}} round-tripped to %#v", out.SlSl)
	}
	if v, ok := out.MpSl["k"]; !ok || v == nil || len(v) != 0 {
		t.Errorf("map[string][]string{k:{}} round-tripped to %#v", out.MpSl)
	}
	if v, ok := out.MpMp["k"]; !ok || v == nil || len(v) != 0 {
		t.Errorf("map[string]map[string]string{k:{}} round-tripped to %#v", out.MpMp)
	}
	if out.PtrSl == nil || len(*out.PtrSl) != 0 {
		t.Errorf("*[]string pointing at an empty slice round-tripped to %#v", out.PtrSl)
	}
	if out.PtrMp == nil || len(*out.PtrMp) != 0 {
		t.Errorf("*map[string]string pointing at an empty map round-tripped to %#v", out.PtrMp)
	}
	if len(out.SlPtrSl) != 1 || out.SlPtrSl[0] == nil || len(*out.SlPtrSl[0]) != 0 {
		t.Errorf("[]*[]string holding a pointer to an empty slice round-tripped to %#v", out.SlPtrSl)
	}

	// A zero M emits nothing for any of these, and reads back as zero -- with
	// one documented exception, below.
	want := matrix.M{}
	want.ArSl = [2][]string{{}, {}}
	zero := roundTripM(t, &matrix.M{})
	if !reflect.DeepEqual(*zero, want) {
		t.Errorf("the zero value did not round-trip: %#v", *zero)
	}
}

// TestMatrix_ArraySlotsAreAlwaysWritten states the one place nil and empty
// cannot be told apart, and the shape that fixes it.
//
// A fixed-size array has no absent slot: every one of its N positions is written,
// so a nil slice in a slot goes out as [] and comes back empty. That is not a
// codec bug, it is what a fixed array means -- and the answer is the same as for
// every other "absent versus empty" question in this package, a pointer.
func TestMatrix_ArraySlotsAreAlwaysWritten(t *testing.T) {
	slot := []string{"here"}
	in := &matrix.M{
		ArSl:    [2][]string{nil, {"x"}},
		ArPtrSl: [2]*[]string{nil, &slot},
	}
	out := roundTripM(t, in)

	if out.ArSl[0] == nil || len(out.ArSl[0]) != 0 {
		t.Errorf("a nil slice in an array slot came back as %#v, want an empty slice", out.ArSl[0])
	}
	if len(out.ArSl[1]) != 1 || out.ArSl[1][0] != "x" {
		t.Errorf("the occupied slot round-tripped to %#v", out.ArSl[1])
	}

	if out.ArPtrSl[0] != nil {
		t.Errorf("a nil pointer in an array slot came back as %#v, want nil", out.ArPtrSl[0])
	}
	if out.ArPtrSl[1] == nil || len(*out.ArPtrSl[1]) != 1 {
		t.Errorf("the occupied pointer slot round-tripped to %#v", out.ArPtrSl[1])
	}
}

// TestMatrix_ArrayRejectsTooManyElements: a document holding more elements than
// the array has slots is an error, not a truncation. Dropping the tail would be
// the codec deciding which of the writer's data does not matter.
func TestMatrix_ArrayRejectsTooManyElements(t *testing.T) {
	node, err := parse.Parse([]byte("ar:\n- a\n- b\n- c\n"))
	if err != nil {
		t.Fatal(err)
	}
	out := &matrix.M{}
	err = out.FromTonyIR(node)
	if err == nil {
		t.Fatalf("three elements were accepted into a [2]string, giving %#v", out.Ar)
	}
	if !strings.Contains(err.Error(), "[2]string") {
		t.Errorf("error does not name the type that did not fit: %v", err)
	}
}

// TestMatrix_GeneratedAgreesWithReflection: gomap's reflection path reads types
// without generated codecs, and the two must not disagree about what a document
// means. The generated encoder's output is decoded by reflection and vice versa.
func TestMatrix_GeneratedAgreesWithReflection(t *testing.T) {
	in := fullM()

	genNode, err := in.ToTonyIR()
	if err != nil {
		t.Fatalf("generated ToTonyIR: %v", err)
	}
	viaReflection := &matrix.M{}
	if err := gomap.FromTonyIR(genNode, viaReflection); err != nil {
		t.Fatalf("reflection could not read what codegen wrote: %v", err)
	}
	if !reflect.DeepEqual(*in, *viaReflection) {
		t.Errorf("reflection read codegen's output differently:\n got %#v\nwant %#v", *viaReflection, *in)
	}

	reflNode, err := gomap.ToTonyIR(in)
	if err != nil {
		t.Fatalf("reflection ToTonyIR: %v", err)
	}
	viaCodegen := &matrix.M{}
	if err := viaCodegen.FromTonyIR(reflNode); err != nil {
		t.Fatalf("codegen could not read what reflection wrote: %v", err)
	}
	if !reflect.DeepEqual(*in, *viaCodegen) {
		t.Errorf("codegen read reflection's output differently:\n got %#v\nwant %#v", *viaCodegen, *in)
	}
}
