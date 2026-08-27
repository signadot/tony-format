# encode: an array's FIRST element loses its head comment to the array

A head comment on an array's first element is written where it reads back as a
comment on the ARRAY. One `o v -c` pass moves it, and the result is a fixed point,
so nothing ever indicates it happened. `o v -w` writes it into the file.

```
$ printf -- '- # c\n  5\n' | o dump -c -
{
  type: Array
  values: [
    { type: Comment  values: [ { type: Number  int: 5 } ]  lines: ["# c"] }
  ]
}

$ printf -- '- # c\n  5\n' | o v -c | o dump -c -
{
  type: Comment
  values: [ { type: Array  values: [ { type: Number  int: 5 } ] } ]
  lines: ["# c"]
}
```

`Array[ Comment{5} ]` became `Comment{ Array[5] }`. The comment now annotates the
container instead of the element.

It also MERGES with a comment that was already on the container, so two comments
about two different things become one comment about the outer one:

```
$ o v -c <<EOF
# HELLO!
- # HELLO
  | # hello
    I am an ape
EOF
# HELLO!
# HELLO
- | # hello
  I am an ape
```

## it is only the first element

A head comment on any LATER element round-trips exactly, because the line above
its `- ` is not the array's own comment position:

```
- 1
# c
- 2      ->  Array[1, Comment{2}]  ->  written back the same  ->  same IR
```

So the defect is confined to index 0, where `# c` above the first `- ` is also the
spelling for a comment on the array.

## where it comes from

`writeElementHeadComment` (encode/encode.go) does this deliberately, and its doc
comment states the reason:

> The two spellings share one IR, so only one can survive a round trip; this is
> the one the format's own examples use.

The premise is false. They do not share one IR: `- # c` puts the comment on the
element, `# c` above the `- ` puts it on the array. Writing the first as the second
is not a choice of spelling, it is a change of what the comment is about.

Nested under a field it is the same: `a:` then `- # c` then `  1` comes back with
the comment on the value of `a`.

## the fork

**(a) fix the encoder.** For the first element only, write `- # c` and put the value
on the following line. Later elements keep the above-the-marker spelling, which
already round-trips and is the style the docs use. Both meanings survive, and the
change is confined to the one ambiguous position.

**(b) fix the parser.** Read a comment above an array's first `- ` as the first
ELEMENT's, making the two spellings genuinely share one IR as the code comment
claims. Costs the ability to comment an array at document root, where a header
comment is a reasonable thing to want.

(a) is the smaller change and loses nothing. Recorded rather than fixed pending a
decision on which.

Found while checking a block literal's opening-line comment; predates e295310 and
93fe8dd, verified against e295310^.