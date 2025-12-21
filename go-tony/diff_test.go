package tony

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/parse"
)

type diffTest struct {
	a    string
	b    string
	diff string
}

var diffTests = []diffTest{
	{
		a: `
f1: a
f2: a
f3: a
f4: a
f5:
  f5a: 1
  f5b: !b2 2`,
		b: `
f0: b
f1: b
f2: b
f5:
  f5a: 1`,
		diff: `
f0: !insert b
f1: !replace
  from: a
  to: b
f2: !replace
  from: a
  to: b
f3: !delete a
f4: !delete a
f5:
  f5b: !delete(b2) 2`,
	},
	{
		a: `
- 1
- 2
- 3
- 3
- 3
- 7
- 8`,
		b: `
- 2
- 3
- 3
- 3
- 4
- 7
- 9`,
		diff: `
!arraydiff
0: !delete 1
5: !insert 4
7: !replace
  from: 8
  to: 9`,
	},
	{
		a: `
- 1
- 2
- hello 
- hello
- hellp 
- 7
- 8`,
		b: `
- 2
- hello
- hello 
- hello
- 4
- 7
- 9`,
		diff: `
!arraydiff
0: !delete 1
4: !replace
  from: hellp
  to: hello
6: !insert 4
8: !replace
  from: 8
  to: 9`,
	},
	{
		a: `
f1: hello
f2: a
f3: a
f4: a
f5:
  f5a: 1
  f5b: 2`,
		b: `
f0: b
f1: he11o
f2: b
f5:
  f5a: 1`,
		diff: `
f0: !insert b
f1: !strdiff(false)
  2: !replace
    from: ll
    to: "11"
f2: !replace
  from: a
  to: b
f3: !delete a
f4: !delete a
f5:
  f5b: !delete 2
`,
	},
	{
		a: `hello`,
		b: `he11o`,
		diff: `
!strdiff(false)
2: !replace
  from: ll
  to: "11"`,
	},
	{
		a:    `!a hello`,
		b:    `!b hello`,
		diff: `!retag(a,b) null`,
	},
	{
		a: `!a hello`,
		b: `!b hell0`,
		diff: `
!strdiff(false).retag(a,b)
4: !replace
  from: o
  to: "0"`,
	},
	{
		a: `
- 1
- 2
- !h 3
- 4
- 5`,
		b: `
- 1
- 2
- 4
- 5`,
		diff: `
!arraydiff
2: !delete(h) 3
`,
	},
	{
		a: `
- 1
- 2
- 3
- 4
- 5`,
		b: `
- 1
- 2
- 4
- !j 5`,
		diff: `
!arraydiff
2: !delete 3
4: !addtag(j) null`,
	},
	{
		a: `
!key(name)
- name: 1
- name: 2
- name: 3
- name: 4
- name: 5`,
		b: `
!key(name)
- name: 1
- name: 2
- name: 4
- !j 
  name: 5`,
		diff: `
!key(name)
- !delete
  name: 3
- !addtag(j)
  name: 5`,
	},
	{
		a: `
!key(name)
- name: 1
- name: 2
- name: 3
- name: 4
- name: 5`,
		b: `
!key(name)
- name: 1
- name: 2
- name: 4
- name: !j 5`,
		diff: `
!key(name)
- !delete
  name: 3
- name: !addtag(j) 5`,
	},
	{
		a: `
f1: 1
f2: !j 2`,
		b: `
f1: 1
f2: 2`,
		diff: `
f2: !rmtag(j) null`,
	},
	{
		a: `
f1: 1
f2: 2`,
		b: `
f1: 1
f2: !j 2`,
		diff: `
f2: !addtag(j) null`,
	},
	{
		a: `
f0:
  f1: 1
  f2: 2`,
		b: `
f0:
  f1: 1
  f2: !j 2`,
		diff: `
f0:
  f2: !addtag(j) null`,
	},
}

func TestDiff(t *testing.T) {
	for i := range diffTests {
		diffTest := &diffTests[i]
		a, err := parse.Parse([]byte(diffTest.a))
		if err != nil {
			t.Error(err)
			continue
		}
		b, err := parse.Parse([]byte(diffTest.b))
		if err != nil {
			t.Error(err)
			continue
		}

		diff := Diff(a, b)
		if diff == nil {
			if diffTest.diff == "" {
				continue
			}
			t.Errorf("got no diff, expected\n%s\n", diffTest.diff)
			continue
		}
		got := strings.TrimSpace(encode.MustString(diff))
		want := strings.TrimSpace(diffTest.diff)
		if got != want {
			t.Errorf("# got\n%q\n---\n# want\n%q\n", got, want)
		}
		rev, err := libdiff.Reverse(diff)
		if err != nil {
			t.Error(err)
			continue
		}
		revrev, err := libdiff.Reverse(rev)
		if err != nil {
			t.Error(err)
			continue
		}
		want = strings.TrimSpace(encode.MustString(revrev))
		if got != want {
			t.Errorf("# orig diff\n%s\n---\n# rev diff\n%s\n---\n# rev rev\n%s\n",
				encode.MustString(diff), encode.MustString(rev), encode.MustString(revrev))
		}
	}
}
