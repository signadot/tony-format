package parse

import (
	"bytes"
	"math"
	"math/rand"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
)

// reparseNumber returns what a number node becomes after encode -> parse.
func reparseNumber(t *testing.T, n *ir.Node) (*ir.Node, error) {
	t.Helper()
	doc := ir.FromMap(map[string]*ir.Node{
		"a_number": n,
		"z_after":  ir.FromString("sibling"),
	})
	var b bytes.Buffer
	if err := encode.Encode(doc, &b, encode.EncodeFormat(format.TonyFormat)); err != nil {
		return nil, err
	}
	got, err := Parse(b.Bytes())
	if err != nil {
		return nil, err
	}
	return ir.Get(got, "a_number"), nil
}

// sameNumber reports whether a reparsed number is the one that went in, int and float being
// different values even when they print alike.
func sameNumber(in, out *ir.Node) bool {
	if out == nil || out.Type != ir.NumberType {
		return false
	}
	switch {
	case in.Int64 != nil:
		return out.Int64 != nil && *out.Int64 == *in.Int64
	case in.Float64 != nil:
		return out.Float64 != nil && *out.Float64 == *in.Float64
	}
	return false
}

// A float must come back a float with the same value. It did not: 'f' formatting wrote 1.0
// as "1" and 1e2 as "100", which reparse as integers, and it wrote the largest float64 as
// a 309 digit decimal expansion that overflows int64 and does not parse at all -- the
// encoder emitting a document its own parser rejects.
func TestFloatRoundTrip(t *testing.T) {
	for _, f := range []float64{
		0, 1, -1, 1.5, -2.5, 0.1, 100, 1e2, 3.0e2, 2.5e-3,
		math.MaxFloat64, -math.MaxFloat64, math.SmallestNonzeroFloat64,
		1e21, 1e-21, 1 << 62, math.Pi,
	} {
		in := ir.FromFloat(f)
		out, err := reparseNumber(t, in)
		if err != nil {
			t.Errorf("%v: %v", f, err)
			continue
		}
		if !sameNumber(in, out) {
			t.Errorf("%v came back type=%v int=%v float=%v", f, out.Type, out.Int64, out.Float64)
		}
	}
}

func TestIntRoundTrip(t *testing.T) {
	for _, i := range []int64{
		0, 1, -1, 42, -42, math.MaxInt64, math.MinInt64, 10000000005,
	} {
		in := ir.FromInt(i)
		out, err := reparseNumber(t, in)
		if err != nil {
			t.Errorf("%d: %v", i, err)
			continue
		}
		if !sameNumber(in, out) {
			t.Errorf("%d came back type=%v int=%v float=%v", i, out.Type, out.Int64, out.Float64)
		}
	}
}

// TestNumberRoundTripRandom is the shape no hand-written list covers: arbitrary bit
// patterns, including the exponent ranges where the decimal expansion runs long and the
// subnormals where it runs small.
func TestNumberRoundTripRandom(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		f := math.Float64frombits(r.Uint64())
		if math.IsInf(f, 0) || math.IsNaN(f) {
			continue // no Tony syntax; encodeNumber reports these
		}
		in := ir.FromFloat(f)
		out, err := reparseNumber(t, in)
		if err != nil {
			t.Fatalf("%v (bits %#x): %v", f, math.Float64bits(f), err)
		}
		if !sameNumber(in, out) {
			t.Fatalf("%v (bits %#x) came back int=%v float=%v",
				f, math.Float64bits(f), out.Int64, out.Float64)
		}
	}
}

// Infinities and NaN have no Tony syntax. They cannot be parsed into a document, but the Go
// API can build them, and the encoder used to write "+Inf", putting back the unparseable
// output the float formatting was fixed to stop producing.
func TestNonFiniteFloatRejected(t *testing.T) {
	for _, f := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		if _, err := reparseNumber(t, ir.FromFloat(f)); err == nil {
			t.Errorf("%v encoded without error", f)
		}
	}
}

// TestNumberRangeLimitation records where the implementation stops short of the format.
// A Tony number is a JSON number and the format bounds neither its magnitude nor its
// precision, but integers are read as int64 and everything else as float64, so these are
// refused rather than silently rounded.
//
// This pins current behaviour, not a decision: when arbitrary precision numbers arrive
// this test is what says so.
func TestNumberRangeLimitation(t *testing.T) {
	for _, doc := range []string{
		"k: 1e400\n",                          // beyond float64
		"k: 123456789012345678901234567890\n", // beyond int64, JSON rounds it
		"k: 9223372036854775808\n",            // int64 max + 1
		"k: -9223372036854775809\n",           // int64 min - 1
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%q parsed; expected the int64/float64 limitation to reject it", doc)
		}
	}
}

// Equal nodes must hash equally. -0.0 and 0.0 are DeepEqual, because that uses Go's ==
// where they compare equal, but their Float64bits differ by the sign bit -- so anything
// keyed on Hash could hold both as distinct entries while every comparison said they were
// the same value.
func TestSignedZeroEqualAndHash(t *testing.T) {
	z := 0.0
	neg, pos := ir.FromFloat(-z), ir.FromFloat(z)
	if math.Signbit(*neg.Float64) == math.Signbit(*pos.Float64) {
		t.Fatal("test setup: the sign bit was folded away")
	}
	if !neg.DeepEqual(pos) {
		t.Error("DeepEqual(-0.0, 0.0) = false")
	}
	if neg.Hash() != pos.Hash() {
		t.Errorf("Hash(-0.0) = %d, Hash(0.0) = %d, want equal", neg.Hash(), pos.Hash())
	}
	// One value, one text.
	for _, n := range []*ir.Node{neg, pos} {
		out, err := reparseNumber(t, n)
		if err != nil {
			t.Fatal(err)
		}
		if !out.DeepEqual(pos) {
			t.Errorf("signed zero did not round-trip to the same value: %v", out.Float64)
		}
	}
}
