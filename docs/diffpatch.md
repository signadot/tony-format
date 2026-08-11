# Diff and Patch

Diffing and Patching YAML

WIP

## Goals

YTool diffing support strives to 

1. Create succint but comprehensive and readable yaml diffs
    a. include LCS computation on arrays
    b. include string-to-string diffs
1. Work with yaml tags
1. Interoperate with the existing patching mechanism
    a. work for !key(name) -- see [Keyed Arrays](#keyed-arrays)
1. Include string-to-string diffs 

## Format

### Basic Format

```yaml
a:
  b: 
  # this indicates there's an array in from and to
  # here which differs

  !arraydiff  
 
  # differs one at index 172 (hex 0xac)
  # with a replacement
  00ac: !replace
    from: w
    to: x

  # delete v in from 
  c: !delete v
  # insert x in to
  d: !insert x

  # line-by-line strdiff
  e: !strdiff(true)

  # char-by-char strdiff
  f: !strdiff(false)
```

Anywhere there is a diff we can generate a forward patch by eliding from
and embedding to where the diff node is.  Likewise in reverse, since a diff
can be reversed.

### Tag Format 

In the basic format, tags are preserved in any `!replace` operation,


## Keyed Arrays

When both sides of a diff carry `!key(f)` with the same field, elements are matched by
their key rather than by position, and the diff names the elements which changed:

```tony
items: !key(sku)
- !insert
  sku: G
  q: 3
- !delete
  sku: W
  q: 1
```

Applying that to a list which has since gained or lost *other* elements leaves those
alone, which is the point: position-based diffs cannot do this.  A reordering of the
same elements is not a difference at all.

Two properties worth stating, since neither is obvious:

- The keyed branch is taken only when **both** sides carry `!key(f)` with the *same*
  field, the field is a resolvable path, and each element has it.  Any miss falls back
  to a positional array diff, silently -- so two documents keyed by different fields
  diff by position rather than reporting a conflict.
- `Patch(a, Diff(a, b))` reproduces `b`'s content, but not necessarily node-for-node:
  object fields are normalised to alphabetic order, and presentation tags such as
  `!bracket` are dropped when a value is created rather than merged into an existing
  one.  Compare with a diff, not with a deep equality.

## String Diffs

String diffs are computed rune by rune unless both sides have multiple lines,
in which case they are computed line by line.

When strings differ and the length of the text in the differences is at least
half the minimum length of the source and target text, the texts are simply
replaced wholesale w/out a string diff.  This is a heuristic that will
need improving over time, as some strings are really representing enums and
the like under the hood and here in-string diffs don't help readability.

String diffs do not apply to fields.


