# Tony Format

Tony is a dialect of YAML which strives to provide improved ergonomics and
safety through simplicity and coherent tooling.

## Overview

### Tony and JSON

Valid JSON is valid Tony with no changes in semantics.

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

Like Go, Tony defines a single normalized form for human editing.  This form
has exactly 1 degree of freedom: a subtree may optionally be bracketed.
Otherwise, everything, including indentation, is fixed.

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
- may not start with a digit.
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
# same as " <\n^ leading space\n"
| 
   <   
  ^ leading space
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
