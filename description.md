# o: -trim is a document on get and list and a bool on match, so the same words mean two things

`-trim` is a string on `get` and `list` and a bool on `match`. The three commands share one
vocabulary of match documents -- and say so in their help -- so the same flag meaning two
things in it is a trap rather than a difference.

## What each means

```
o get -trim '{name: null}' a doc.tony     # a, with only the parts {name: null} names
  {
    name: x
  }

o match -trim '{state: open}' s.tony      # the matching documents, trimmed TO the pattern
  {
    state: open
  }
```

get and list take a document and answer "how much of each node". match takes no value and
answers "trim to the pattern I am already matching with".

## How it goes wrong

Nothing warns, because both spellings parse. On match, `-trim` consumes nothing, so the
document meant for it becomes the PATTERN and the pattern becomes a FILENAME:

```
$ o match -trim '{name: null}' '{state: open}' s.tony
error matching {state: open}: error opening {state: open}: no such file or directory
```

Exit 2, and the message names the pattern as a file that does not exist -- which is true, and
tells the reader nothing about the flag that caused it. The same words in the same order mean
different things depending on which of two sibling commands is running.

## Shape of a fix

The names should not collide. `-trim <document>` is the one that generalises -- it is the
question "how much of each node", which every one of the three can answer -- so it should mean
that everywhere, and match's valueless form should be spelled as its own thing:

    -trim <doc>    write only the parts this match document names   get, list, match
    -fit           trim to the pattern being matched                match

That also gives match something it cannot say today: trim a matched document to a shape OTHER
than the pattern that selected it.

The alternative -- making match's `-trim` take an optional value -- reproduces the trap rather
than fixing it, since whether the next argument is the value or the pattern is exactly the
ambiguity above.

Naming is the decision; either name is a flag day for anyone with `-trim` in a script, which is
the argument for doing it while `o` is young.