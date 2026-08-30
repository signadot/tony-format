package tony

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

type patchTest struct {
	Doc   string
	Patch string
	Res   string
	Error error
	X     bool
}

func TestPatch(t *testing.T) {
	tests := []patchTest{
		{
			Doc:   `a: 1`,
			Patch: "b: 1",
			Res:   "a: 1\nb: 1",
		},
		{
			Doc:   `a: 1`,
			Patch: "a: null",
			Res:   "a: null",
		},
		{
			Doc:   `a: 1`,
			Patch: "a: !delete null",
			Res:   "{}",
		},
		{
			Doc: `
- 1
- 2`,
			Patch: `
- !delete 1
- 2`,
			Res: "- 2",
		},
		{
			Doc: `[1, 2]`,
			Patch: `
- !delete 1
- 2`,
			Res: "[\n  2\n]",
		},
		{
			Doc:   `[1, 2]`,
			Patch: "- !delete 1\n- 2",
			Res:   "[\n  2\n]",
		},
		{
			Doc:   `[1, 2]`,
			Patch: "- !delete 1\n- 2\n- 3",
			Res: `
[
  2
  3
]`,
		},
		{
			Doc:   `a: 1`,
			Patch: "# s\nstring # s",
			Res:   "string",
		},
		// 7
		{
			Doc:   `[1,2,3]`,
			Patch: `[1,2,4]`,
			Res: `
[
  1
  2
  4
]`,
		},
		/*
		   		{
		   			Doc: `
		   # f1 is f1
		   f1: f1`,
		   			Patch: `
		   # f1 is f2
		   f1: f1
		   `,
		   			Res: `
		   # f1 is f2
		   f1: f1
		   `,
		   		},
		*/
		{
			Doc: `
f1:
- 1
- 2`,
			Patch: `
{}`,
			Res: `
f1:
- 1
- 2`,
		},
		{
			Doc: `
- key: 1
  value: 2

- key: 2
  value: 3`,
			Patch: `
!key(key)
- key: 2
  value: 33`,
			Res: `
- key: 1
  value: 2
- key: 2
  value: 33`,
		},
		// 10
		{
			Doc: `
x:
- key: 1
  value: 2

- key: 2
  value: 3`,
			Patch: `
x: !key(key)
- key: 2
  value: 33`,
			Res: `
x:
- key: 1
  value: 2
- key: 2
  value: 33`,
		},
		{
			Doc: `
dive-x:
- key:
    some-field-1: thing
    with: structure
  value: 2

- key: 2
  value:
    some-field-2: thing
    with: structure`,
			Patch: `
dive-x: !dive
- match: !field.glob 'some-field-*'
  patch:
    crazy: true`,
			Res: `
dive-x:
- key:
    some-field-1:
      crazy: true
    with: structure
  value: 2
- key: 2
  value:
    some-field-2:
      crazy: true
    with: structure`,
		},
		// 12
		{
			Doc: `
embed-me:
- key:
    some-field-1: thing
    with: structure
  value: 2`,
			Patch: `
!embed(X)
- X
- X`,
			Res: `
- embed-me:
  - key:
      some-field-1: thing
      with: structure
    value: 2
- embed-me:
  - key:
      some-field-1: thing
      with: structure
    value: 2`,
		},
		{
			Doc: `
f1: |
  signadot-staging
  alphabet-soup
  chime-staging
`,
			Patch: `
f1: !pipe |
  sed -e s/staging/prod/
`,
			Res: `
f1: |
  signadot-prod
  alphabet-soup
  chime-prod
`,
		},
		{
			Doc: `
f1:
- name: a/b
  value: x
- name: b/c
  value: y
- name: a/c
  value: z`,
			Patch: `
f1: !all
  value: true`,
			Res: `
f1:
- name: a/b
  value: true
- name: b/c
  value: true
- name: a/c
  value: true`,
		},
		// 15
		{
			Doc: `
f1: a
f2: b`,
			Patch: `
!if
if:
  f1: a
then:
  f3: c
else:
  !pass null`,
			Res: `
f1: a
f2: b
f3: c`,
		},
		{
			Doc: `
f1:
- name: a/b
  value: x
- name: b/c
  value: y
- name: a/c
  value: z`,
			Patch: `
f1: !all.if
  if:
    name: !glob a/*
  then:
    value: true
  else: !pass null`,
			Res: `
f1:
- name: a/b
  value: true
- name: b/c
  value: y
- name: a/c
  value: true`,
		},
		//		{
		//			Doc: `
		//f1: a
		//f2: b`,
		//			Patch: `
		//!embed(X)
		//<<: |-
		//  # before
		//<<: X
		//<<: |-
		//  # after`,
		//			Res: `
		//# before
		//f1: a
		//f2: b
		//# after`,
		//			X: true,
		//		},
		{
			Doc: `
'hello world'`,
			Patch: `
!strdiff(false)
2: !delete l
4: !replace
  from: o
  to: p
6: !insert "the "`,
			Res: `
"help the world"`,
		},
		{
			Doc: `
'hello world'`,
			Patch: `
!strdiff(false)
2: !delete l
4: !replace
  from: o
  to: p
6: !insert "the "
15: !insert "!"
`,
			Res: `
"help the world!"`,
		},
		{
			Doc: `|
  h
  e
  l
  l
  o
  
  w
  o
  r
  l
  d
`,
			Patch: `
!strdiff(true)
2: !delete l
4: !replace
  from: o
  to: p
6: !insert "the\n"`,
			Res: `|
  h
  e
  l
  p
  
  the
  
  w
  o
  r
  l
  d
`,
		},
		{
			Doc: `
- name: a
- name: b`,
			Patch: `
!key(name)
- name: !j a`,
			Res: `
- name: !j a
- name: b`,
		},
		{
			Doc: `
- a
- b`,
			Patch: `
!arraydiff
1: !delete b`,
			Res: `
- a`,
		},
		{
			Doc: `
- a
- b`,
			Patch: `
!arraydiff
1: !insert(foo(bar,baz)) c`,
			Res: `
- a
- !foo(bar,baz) c
- b`,
		},
		{
			Doc: `
a:
  b: c
d: e`,
			Patch: `
!field(a,A).field(d,D) null`,
			Res: `
A:
  b: c
D: e`,
		},
		{
			Doc: `
a:
  b: c
d: e`,
			Patch: `
z: !insert 1
zz: !insert 2`,
			Res: `
a:
  b: c
d: e
z: 1
zz: 2`,
		},
		{
			Doc: `
a: { b: c }
d: e`,
			Patch: `
z: !insert 1
zz: !insert 2`,
			Res: `
a: {
  b: c
}
d: e
z: 1
zz: 2`,
		},
		{
			// A patch element that does not carry the merge key is
			// unmergeable: it matches nothing, and appending it anyway would
			// be worse than refusing.
			Doc: `
- name: c
  image: x`,
			Patch: `
!key(name)
- imagePullPolicy: Never`,
			Error: errors.New("patch element has no key"),
		},
		{
			// A *document* element without the key is a different matter: no
			// patch element can address it, so it rides along untouched.
			Doc: `
- image: x
- name: c
  image: y`,
			Patch: `
!key(name)
- name: c
  imagePullPolicy: Never`,
			Res: `
- image: x
- image: y
  imagePullPolicy: Never
  name: c`,
		},
	}
	for i := range tests {
		test := &tests[i]
		a, err := parse.Parse([]byte(test.Doc))
		if err != nil {
			t.Errorf("error decoding doc in test %d: %v", i, err)
			continue
		}
		b, err := parse.Parse([]byte(test.Patch))
		if err != nil {
			t.Errorf("error decoding patch in test %d: %v", i, err)
			continue
		}
		t.Logf("# decoded patch\n%s\n", encode.MustString(b))
		patched, err := Patch(a, b)
		if err != nil {
			if test.Error == nil {
				t.Errorf("test case %d: unexpected error %v", i, err)
			}
			continue
		}
		t.Logf("patch path %q\n", patched.Path())
		t.Logf("patched:\n%s", encode.MustString(patched))
		if test.Error != nil {
			t.Errorf("test case %d: expected error %v", i, test.Error)
			continue
		}
		buf := bytes.NewBuffer(nil)
		if err := encode.Encode(patched, buf, encode.InjectRaw(test.X)); err != nil {
			t.Errorf("error encoding patched result on test %d: %v", i, err)
		}
		got := strings.TrimSpace(buf.String())
		want := strings.TrimSpace(test.Res)
		if got != want {
			t.Logf("got\n`%q`\n", got)
			t.Logf("want\n`%q`\n", want)
			for i := 0; i < min(len(got), len(want)); i++ {
				if got[i] != want[i] {
					t.Logf("at %d %q != %q", i, string(got[i]), string(want[i]))
					break
				}
			}

			t.Fail()
			continue
		}
	}
}

// TestKeyedListNoKey covers the crash reported in bzf782sph12krsmwe5n0: a
// !key(name) element that omits the key. GetPath answers a missing field with
// (nil, nil), and the nil went unchecked, so the process died on a nil deref
// where it should have said what was wrong. The table above pins the
// behaviour; this pins the message, which is the whole point of the fix.
func TestKeyedListNoKey(t *testing.T) {
	doc, err := parse.Parse([]byte("- name: c\n  image: x"))
	if err != nil {
		t.Fatal(err)
	}
	patch, err := parse.Parse([]byte("!key(name)\n- imagePullPolicy: Never"))
	if err != nil {
		t.Fatal(err)
	}

	// Patch: an error, not a panic.
	_, err = Patch(doc, patch)
	if err == nil {
		t.Fatal("a patch element without its merge key must be an error")
	}
	for _, want := range []string{`!key(name)`, "element 0", `"name"`, "imagePullPolicy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}

	// Match reaches the same key lookup and must not panic there either.
	if _, err := Match(doc, patch); err == nil {
		t.Error("matching against a patch element without its merge key must be an error")
	}

	// The document side is not an error: an element that carries no key is
	// simply not one of the ones being merged, and stays put.
	doc2, err := parse.Parse([]byte("- image: x\n- name: c"))
	if err != nil {
		t.Fatal(err)
	}
	patch2, err := parse.Parse([]byte("!key(name)\n- name: c\n  imagePullPolicy: Never"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Patch(doc2, patch2)
	if err != nil {
		t.Fatalf("a document element without the key must ride along, not fail the patch: %v", err)
	}
	got := strings.TrimSpace(encode.MustString(res))
	want := "- image: x\n- imagePullPolicy: Never\n  name: c"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
