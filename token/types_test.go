package token

import (
	"testing"
)

type tsTest struct {
	in, out string
}

func TestTypesString(t *testing.T) {
	var tss = []tsTest{
		{in: `"abc"`, out: `abc`},
		{in: `"\"'"`, out: `"'`},
		{in: `'"'`, out: `"`},
		{in: `"\t"`, out: "\t"},
		{in: `'\u221e'`, out: "∞"},
	}
	for _, ts := range tss {
		toks, err := Tokenize(nil, []byte(ts.in))
		if err != nil {
			t.Error(err)
			continue
		}
		if ts.out != toks[0].String() {
			t.Errorf("got %q want %q", toks[0].String(), ts.out)
		}
	}
}

func TestTypesMLit(t *testing.T) {
	var mlts = []tsTest{
		{in: `|
  abc
  def
`, out: "abc\ndef\n"},
		{in: `|-
  abc
  def
`, out: "abc\ndef"},
		{in: `|
  abc
  def
`, out: "abc\ndef\n"},
		{in: `|-
  abc
  def
`, out: "abc\ndef"},
		{in: `|-
  abc
  def

  ghi
`, out: "abc\ndef\n\nghi"},
		{in: `|-


  abc
  def

  ghi
`, out: "\n\nabc\ndef\n\nghi"},
	}
	for _, ts := range mlts {
		toks, err := Tokenize(nil, []byte(ts.in))
		if err != nil {
			t.Error(err)
			continue
		}
		if ts.out != toks[0].String() {
			t.Errorf("From: %s\n--\ngot %q want %q bytes %q", ts.in, toks[0].String(), ts.out, toks[0].Bytes)
		}
	}
}
