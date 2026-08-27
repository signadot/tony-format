# Tony Format

Tony is a dialect of YAML which strives to provide improved ergonomics and
safety through simplicity and coherent tooling.

## Overview

### Tony and JSON

Valid JSON is valid Tony with no changes in semantics.  The current
implementation does not yet reach that for numbers of very large magnitude or
precision; see [Number Range](#number-range).

Tony extends this base by providing a human friendly syntax which is ergonomic
and clear.  In most cases the extensions to JSON syntax do not in any form
change the base structure of JSON semantics.  They exist to make life easier
for humans to read, share, and edit JSON.

There is one notable exception: Tony also supports local [YAML
tags](https://yaml.org/spec/1.2.2/#24-tags).  These tags are used in Tony
evaluation, diffs, patches and matches, the core coherent tooling mechanisms
provided with Tony.  Tags are meta in this sense, they are not primarily
intended to be part of the core underlying data. Rather, they used primarily
for transforms of the core underlying data.

### Tony and YAML

A large subset of Tony is valid YAML. YAML is huge and context-sensitive
and Tony has no intention of adopting its idiosyncractic
labyrinth of specifications.  Rather the Tony format is designed to be 
less context sensitive and safer with respect to [known bugs](https://news.ycombinator.com/item?id=22847940)
 and confusion that can result from YAML.

While there's plenty of room for improvement in YAML in these respects, and it
seems any path forward for YAML which improves this will be far too complex,
compatability with existing YAML is a necessary condition for many.

To address this:

- The Tony format is a dialect of YAML that includes (optionally) local YAML
  tags.  Under certain very commonly occuring restrictions, valid Tony format
  is valid YAML.  It may "just work" for your use case.
- Our initial tooling supports support reasonable "YAML" output 
  and supports YAML as found in common usage, such as Kubernetes YAML, KYAML, 
  GHA, etc.


### Block Style

Like YAML, Tony permits mixing indentation based block style with explicit
bracketing using '{' and '['.  Unlike YAML, indentation under explicit
bracketing can still be exploited for constructs like block literals.

### Normalization

Like Go, Tony defines a single normalized form for human editing.  Its only
degrees of freedom are the [presentation tags](#presentation-tags): whether a
subtree is bracketed, whether a string is written as a block literal, and which
notation a number is written in.  Otherwise everything, including indentation,
is fixed.

What a reader ACCEPTS is deliberately wider than what a writer PRODUCES, so that a
document part-way through being edited is still a document.  Whitespace running to
the end of a line after a value, a line holding nothing but whitespace, a blank line
between pairs, a comment or trailing space on a block literal's opening line, and a
quoted string which needs no quotes are all read without complaint.  None of them
survives being written back: the writer strips trailing whitespace, drops blank
lines, and quotes a string only where the value requires it.

So normalizing a document is reading it and writing it out, which is what `o view`
(`o v`) does.  `o v -w` writes the result back over each file, leaving a file already
in normal form untouched.

Comments are the one thing the two disagree about, deliberately.  `o v` PRINTS a view
and leaves them out unless asked with `-c`, as every command in the tool does; `o v
-w` REPLACES the document and always keeps them, because a document includes its
comments and a formatter that drops them is not one.  So `o v file` and `o v -w file`
differ on a commented file, and `o v -c file` is what matches what `-w` writes.

Tony also supports a single normalized wire format form, which uses bracketed
style and contains no newlines within a subtree.  

Both wire format and human interaction format are part of the same language
definition; no directives are required to parse either form, and both forms
have exactly the same non-comment information encoded.  All tooling readily
supports both formats.  By default, human interaction format is used.

### Fun Fact

Tony is "ynot" backwards, and "ynot" may or may not be short for "y notation".

## Atomic Types

Like JSON, Tony provides atomic types or grammatical elements.  JSON types for
`null`, Booleans, strings, and numbers are Tony types.  All of them are
expressed identically in JSON and Tony.

### Strings

JSON strings are Tony strings and also valid YAML strings.  Tony introduces the
following extensions to JSON strings, to facilitate clarity of presentation
and ease of editing.

### Literals

Tony allows expressing strings without quotations somewhat liberally.  A
literal

- may contain unicode digits, letters, and graphics.
- may not contain white space or unicode control characters.
- may start with a digit only in the shapes given in [Numbers](#numbers), and
  then may not contain a ':'.
- may not contain punctuation unless it follows the rules described below.

#### Literals and Punctuation

The only punctuation allowed in any form in a literal are

```tony
# all possible tony punctuation found in a literal
{ '(' ')' '[' ']' '{' '}' '$' '~' '@' ':' '/' '.' '_' '+' '-' '\\' '*' '%' '!' '=' }
```

Of these characters, some may not appear at the beginning:

```tony
# invalid initial characters
{ '[' ']' '{' '}' ':' '-' '!' }
```

If a maximal sequence of valid literal characters are present and unquoted, and
that sequence terminates with `:` or `,`, the final `:` or `,` is not
considered part of the literal, rather part of the key:value Tony grammar or
an element separate in bracketed mode.

```tony
# map of literals to json strings of the same string value
{ 
  a:b: "a:b"
  .[x]: ".[x]"
  $y: "$y"
}  
```

For the open-close paired characters 

```tony
# paired characters
- open: "{"
  close: "}"
- open: "["
  close: "]"
```

A contiguous sequence of valid literal characters is truncated just prior
to the first un-opened close character to form a literal.

For example

```tony
# below, the sequence 'a:b}' contains all valid literal characters
# but since '}' is un-opened, the literal is truncated to 'a:b'.
{a:b}
```

In short, it should suffice to ensure key-value pairs are separated by a ':'
_followed immediately_ by whitespace when working with literals.

Tony automatically deduces which strings can be represented by literals and
does so exactly in those cases, unless asked to produce JSON.

#### Comparison to YAML

This definition of literal syntax is vastly simpler than that of YAML and
nearly as permissive.  However, unlike YAML Tony restricts literals to those
without whitespace. On the other hand, Tony literals have the same definition
in bracketed mode and not.

#### Block Literals

Tony supports a variant of YAML `|` block literals, which is more restrictive
in one sense and more flexible and uniform in another.  

For those unfamiliar with YAML block literals, the idea is that when '|' is the 
end of a line, all the content that follows and is indented to the next level
may be expressed free of quotations and escaping and what not as one big string.

Here are some basic examples:

```tony
|
  hello
  I am a block literal
---
|-
  block literal with trailing end of line chopped off
```

Unlike YAML, in Tony, block literals may be used in bracketed mode
when there is also indentation:

```tony
{
  k: |
    hello
    I am a block literal
}
---
{
  k:
    |
      hello
      I am a block literal
}
---
[
|
  hello
  I am a block literal
null
]
```

Another notable difference is with respect to leading white space.  Since
in YAML, the indentation is variable at different levels of the hierarchy,
it is not possible to distinguish between leading white space of a block 
literal and the intended indentation.  YAML provides the possibility to use
constructs such as `|+<n>` where `<n>` denotes the amount of indentation
to accomodate this.  Tony simply computes the expected indentation because
indentation is fixed (in YAML indentation may vary from one part of a document
to another).

```tony
# same as " <   \n^ leading space\n"
| 
   <   
  ^ leading space
```

The content keeps its own trailing whitespace, as YAML's does: what the block
literal holds is the bytes after the computed indentation, up to the newline.  Only
the OPENING line is a line in the ordinary sense, so it may carry trailing
whitespace and a comment, and neither is part of the value:

```tony
key: |- # what this holds
  the value
```

Tony block literals do not support folding or any other of the myriad of 
YAML variants on block literals.  Folding is rather supported with multiline 
strings.

### Quoted Strings

JSON strings are Tony strings.  Tony also provides simple, expressive means to
format strings within a document.   Even so, Tony double quoted strings are
JSON strings as well.

#### Quoting

Tony can use single or double quotes.  In the event that single quotes are
used, the escaping grammar is identical to the grammar for double quotes,
except that `\"` is replaced by `\'`.

So, there is only one grammar to learn for quoted strings but one can choose
the quote character to minimise needed escaping.  The normal format will always
choose the quote character which minimizes quoting, breaking ties with a
preference for `"` (it being visually more appealing).


#### Multiline Quoting

Multiline quoting is permitted for any string whose opening quotation character
is the first non whitespace character of the line in which it occurs.

```tony
# multiline capable
a:
  "b c d" 
---
# not multiline capable
a: "b c d"
---
# multiline capable
"b c d"
--- 
# not multiline capable
- "b c d"
---
# multiline capable
- 
  "b c d"
---
# also in bracketed mode
[
  "multiline capable string"
  "second elt or folded?",
  "x",
  "y",
]
```

#### Multiline folding

Multiline capable strings may be folded, which can be convenient for entering
very long lines in a readable and editable fashion:

```tony
# string folding
" all part of"
" the same line"
---
a:
- b: # concatenated/folded
    "all part of "
    " the same line"
    " and even more"
---
# string folding can use mixed quotation characters
# and be used in bracketed mode when there is indentation
{ 
  a: [
    {
      b:
        "all part of "
        ' the same "line"'
    }
  ]
}
```

> Note: Multiline folding means that bracketed mode arrays containing
sequences of strings must be separated by commas if those strings are not
multiline folding strings.

```tony
[
  "help"
  " the"
  " world"
]
---
# above is equal to
- "help the world" 
---
[
  "help",
  " the",
  " world"
]
---
# above is equal to
- help
- " the"
- " world"
```

### Numbers

JSON numbers are Tony numbers.  Where Tony differs is in how it reads an
unquoted scalar that _begins_ with a digit, or with '-' and a digit: it looks at
the whole run rather than at the number-shaped prefix of it.

Let `R` be the maximal literal run at that point, stopping at the first ':',
which is always a key separator because no number contains one.

1. If `R` is entirely a number, it is that number.  A number may be written in
   decimal, or in hexadecimal, octal or binary with a `0x`, `0o` or `0b`
   prefix.
2. Otherwise, if `R` is all digits with a leading zero, it is an error.
3. Otherwise, if `R` contains a letter, it is a string.
4. Otherwise, if `R` is three or more '.'-separated groups of digits, it is a
   string.  Two groups is a float, which rule 1 has already taken.
5. Otherwise it is an error, which names `R`.

```tony
# rule 3: quantities and durations
{ cpu: 100m, memory: 1Gi, timeout: 30s, backoff: 1h30m }
---
# rule 4: versions and addresses
{ version: 1.2.3, addr: 192.168.1.1 }
---
# rule 1: still numbers
{ replicas: 3, ratio: 1.5, big: 1e9, neg: -2.5 }
```

Rules 3 and 4 are there because the values they describe are unavoidable in the
documents Tony is written for: a Kubernetes quantity is digits followed by a
suffix by definition, and so is every duration.

Rule 5 is there because the alternative is to read every digit-leading run that
is not a number as text, which would quietly turn a mistyped number into a
string.  `1_000`, `3..14`, `1.` and `1e+` are typing accidents rather than
values, and they stay loud.

Rule 2 is about `0644`, which is 420 to a YAML 1.1 reader, 644 under the YAML
1.2 core schema, and invalid JSON.  No reading Tony could pick would be right
for all three, so it asks for `0o644`, which is 420 everywhere.  `0xzz` is an
error for a related reason: the prefix says the run is a number, so digits that
do not belong to that base make it a botched one rather than text.

### Number Notation

The notation a number is written in is not part of its value.  `0x1f` **is**
31: the two compare equal, hash equal, and patch as the same number.  What
differs is only how they are written, so the notation is carried as a
[presentation tag](#presentation-tags) -- `!hex`, `!oct`, `!bin`, `!exp` -- alongside
`!bracket` and `!literal`.

```tony
# a diff between 0x1f and 31 is about the notation, not the number
k: !rmtag(hex) null
---
# a diff between 0x1f and 0x20 is about the number
k: !replace
  from: 0x1f
  to: 0x20
```

Because it is presentation, the notation is dropped where the output format has
no syntax for it.  The same document written three ways:

| written | Tony | YAML | JSON |
| --- | --- | --- | --- |
| `0x1f` | `0x1f` | `0x1f` | `31` |
| `0o644` | `0o644` | `0o644` | `420` |
| `0b1010` | `0b1010` | `0b1010` | `10` |
| `1e9` | `1e9` | `1e9` | `1e9` |

YAML keeps the prefixed forms because YAML reads them, and that is the point of
writing `0o644` rather than `0644`: a bare `0644` is 420 to one YAML reader and
644 to another, while `0o644` is 420 to both.

Notation is normalized like everything else, one text per value: digits are
lower case, the sign leads (`-0x1f`), and an exponent carries neither padding
nor a `+` (`1e9`, not `1e+09`).

A key cannot carry a tag, so an integer key has nowhere to record a notation
and must be written in base 10.

A string of any of the rejected shapes is written quoted, and Tony writes it
that way itself.  A digit-leading string containing ':' is also written quoted,
since the run would otherwise stop at the colon.

```tony
{ zip: "007", mask: "0x1f", mode: "0644", range: "30s:60s" }
```

#### Number Range

The format puts no bound on how large or how precise a number may be: a Tony
number is a JSON number.

The current implementation does not go that far.  It reads an integer as an
int64 and any other number as a float64, and rejects what does not fit rather
than silently rounding it:

```
1e400                           # rejected
123456789012345678901234567890  # rejected
9223372036854775808             # rejected, int64 max + 1
```

This is a limitation of the implementation rather than a property of the
format, and arbitrary precision numbers can be added later.  Until then
rejecting is the conservative reading -- for comparison, Go's `encoding/json`
rejects the first two of those and silently rounds the third to
`1.2345678901234568e+29`.

A float that does fit is written so that it reads back as the same float: never
as a bare integer, and in exponent form where a decimal expansion would run to
hundreds of digits.  Infinity and NaN have no Tony syntax; they cannot be
parsed, and encoding a document containing one is an error rather than output
that will not parse.

## Collections

Tony supports 2 kinds of collections, arrays and mappings, corresponding to
JSON arrays and objects.

### Commas

Tony allows but, aside from the multiline folded strings above, does not require ',' sepation of 
elements in an object or an array.

```tony
# all valid
[1 2 3]
[1, 2, 3]
[1, 2, 3,]
[1 2, 3]
---
# invalid
[,]
---
# all valid
{ k: v }
{ k1: v1, k2: v2 }
{ k1: v1 k2: v2 }
{ k1: v1, k2: v2, }
```

No more "missing trailing comma" nor "invalid syntax" for adding a trailing
comma!

### Maps

Tony supports JSON maps and also allows 3 additional constructs.

### Key Sets

In bracketed mode only, a set of keys may be denoted by dropping the ':' and
value after any key.  This is syntactic sugar for associating a null value with
the key.

Dropping the VALUE and keeping the ':' is not this, and is not anything: a `p:` with
no value after it is refused wherever it is written, including as the last pair of a
document.  A tag is not a value, so `p: !delete` is the same case as `p:` -- write
`p: null` or `p: !delete null`, or use the key set above.  (YAML mode reads `p:` as a
null, because YAML does.)

```tony
{1 2 3}
---
# equivalent to
1: null
2: null
3: null
```

```tony
{ a !t b c !tt d }
---
# equivalent to
a: !t null
b: null
c: !tt null
d: null
```

```tony
f:
  {a b c d ee
  gg: |
    nine
  ff:
    "line 1"      # zoo
    'is a "line"' # other zoo
  }
g: 22
h: null
---
# equivalent to
f: {
  a: null
  b: null
  c: null
  d: null
  ee: null
  gg: |
    nine
  ff:
    "line 1"
    'is a "line"'
}
g: 22
h: null
```




### Sparse Arrays

Tony supports integer keyed maps, if all the keys are integers.

```
0: hello
13: other
```

Integer keyed maps should have non-negative integer keys expressed
in base-10 notation and not exceed 32 bits.

These are used in diffs between arrays and between strings.

```tony
# document 1
- 1
- 2
- 3
- 4
- 6
---
# document 2
- 1
- 2
- 3
- 4
- 5
- 6
- 7
---
# Tony diff
!arraydiff
4: !insert 5
6: !insert 7
```

### String merge keys

Tony supports [YAML merge keys](https://yaml.org/type/merge.html) but uses them
in a way incompatible with YAML.  Rather than taking mappings as values, Tony
only supports string values for merge keys.

There is correspondingly an encoding option to inject the merge key values at
the indentation level of the mapping:

```tony
spec:
  metadata:
    annotations:
      # inject helm templates
      <<: |
        {{ range $k, $v := ... }}{{ $k | quote}}: {{ $v | quote -}} {{-end}}
--- 
# encode with -x to generate a Helm chart
spec:
  metadata:
    annotations:
      {{ range $k, $v := ... }}{{ $k | quote}}: {{ $v | quote -}} {{-end}}
```

```
{
  a b c
}
---
a b c
---
a
b
c
---
{"a" "b" "c"}
```


## Tags

Tony uses local [YAML tags](https://yaml.org/spec/1.2.2/#24-tags) which allow
inserting a tag associated with any value in the object hierarchy.  In Tony,
one cannot tag a key in a key:value pair in a map, but all other values can be
tagged.

The syntax of a tag is `!<tag-content>` where `<tag-content>` is any sequence
of non-whitespace characters, as determined by unicode.IsSpace.

One inserts a tag by placing it immediately before a value

```tony
!my-tag 2
!my-list-tag
- 1
- 2
- f: !my-tag # applies to [3, 4]
  - 3
  - 4
- g:
    !my-other-tag # applies to [1,2,3]
    [1,2,3]
```

Tony does not support placing tags on keys in maps, doing so is interpreted as
applying to the map if it is the first map element and block mode, otherwise
as a syntax error.

### Presentation Tags

Most tags say something about the data.  A few say only how it was written, and
those are called _presentation tags_:

| tag | records |
| --- | --- |
| `!bracket` | the subtree is written in bracketed style |
| `!literal` | the string is written as a block literal |
| `!hex` `!oct` `!bin` | the integer's notation — see [Number Notation](#number-notation) |
| `!exp` | the float is written in exponent form |

Two documents differing only in these hold the same data.  So they are the
degrees of freedom the [normalized form](#normalization) allows, and everything
that compares or combines data drops them first: `0x1f` and `31` are equal, hash
equal, and patch as the same number, and a diff between them describes the
notation rather than the value.

They are also dropped when the output format has no syntax for them, which is
why a document holding `0x1f` can still be written as JSON.

A key cannot carry a tag, so a value that needs one to be written back as
itself cannot be used as a key: integer keys are base-10.

### Tag Composition

Once tags are in use, it readily becomes evident that they need some structure
and composition.  For example, a tag which indicates a source file may need
further information to indicate whether or not it is interpreted as a string or
interpreted as object notation.

```
some:
  where:
    deep:
      in:
        a:
          document: !file my-file.yaml
```

Do we really need another tag for that?  Tony tags are delimited 
by '.', and they can be composed

```tony
some:
  where:
    deep:
      in:
        a:
          document: !tovalue.file my-file.yaml
```

The syntactic production for this is
```
<tag-content> ::= <single-tag> [ '.' <single-tag> [ '.' <single-tag> ]... ]
```

### Tag Arguments

Additionally, tags may have parameters associated with them:

```
<single-tag> ::= <tag-name> [ '(' [ <tag-content> [ ',' <tag-content ] ... ] ')']
```

In this way, tags can take other parameterized tags as arguments. 

This mechanism is used in Tony diffs to denote differences in tags:

```tony
# document 1
f: !tag1.tag2(a,b) 22
---
# document 2
f: !tag2(z).other(x) 22
---
# the output of a Tony diff between document 1 and 2
f: !retag(tag1.tag2(a,b),tag2(z).other(x))
```

### Tag Operations

Tags are used in Tony for clear and highly expressive diffing, patching, and
matching operations.  A large set such tags are available.  Additionally The Go
Tony library supports adding custom tags to perform catered actions.

#### Checked operations

Two patch operations read as statements of a result and behave as assertions about
what was already there:

```tony
# verifies the node still equals 0x1f, and fails if it does not
k: !replace
  from: 0x1f
  to: 0x20

# verifies the node's tag is already !tag1.tag2(a,b), and fails if it is not
f: !retag(tag1.tag2(a,b),tag2(z).other(x))
```

That is what a diff wants.  A diff describes the step from one specific document to
another, and applying it to a document which has since moved should say so rather
than quietly overwrite the move.

It is not what a patch re-applied to a moving document wants, since such a patch
meets a document which is *expected* to have changed.  The unconditional forms are
`!insert` -- the value is what results -- `!delete` -- absence is what results --
and `!addtag` / `!rmtag`, which are `!retag`'s two halves without the assertion.

#### Relative operations

A second distinction cuts across the first.  `!strdiff`, `!arraydiff`, `!rename` and
`!jsonpatch` describe a change *relative to* what they meet, so the same operation
applied to two different documents produces two different results.  `!pipe` calls out
to the system, so applying it twice runs it twice.

Anything which stores operations, or replays them against a base which has moved on,
needs both distinctions: the checked ones will refuse, and the relative ones will
quietly mean something else.

## Comments

Comments are indicated by '#' and continue to the end of the line.

This differs from YAML in that a comment need not be preceded by a 
space ' #' when not at the beginning of a line.

Additionally, Tony defines an association between comments and parts of the
document.

Every object, list, and atomic value may have preceding comments and a "line
comment".  Atomic values' line comments are what follow them on the same line.
All subsequent comments are attributed to the preceding comments of the next
value, which may be dedented or higher in the object notation.  All trailing
comments at the end of a document are associated as additional lines of the
"line comment" of the top most element.

A value has one set of preceding comments however many places they were written
in.  A comment above a key and a comment above the first line of the block which
follows it are both attributed to the same value -- it is the next value to begin
in either case -- and they compose as further lines of that value's preceding
comments rather than as a second, separate association.

Comments also are associated with all preceding whitespace on the line on which
they occur.

Tony tools support diffs, patching, and matching comments if so desired.

## White Space

### Indentation

Tony uses 2 width indentation and disallows indentation which is not followed
by a value or comment.

In block mode, when an array is directly contained in a list, the block mode
array element prefix '- ' serves as the indentation.

```tony
f:
- I
- am
- indented
- by
- the
- array
- elements
- token
- "- " 
-
  period
```

### Vertical White Space

Tony normalisation eliminates all unused vertical whitespace in a document.

### Extraneous Indentation

Tony disallows _extraneous indentation_ which is any leading whitespace
of a line that does not have associated content, where content includes
comments.

Moreover, if content is prefixed by indentation, but the indentation does
not match the rules above, then that document will be rejected by Tony.

We have found this to be useful in debugging and cleaning up documents.

## Conclusion

We have thoroughly introduced all aspects of the Tony format and we hope this
serves as a useful reference going forward.
