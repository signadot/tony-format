# encode: an element's head comment of several lines is written with no break between them, so it re-reads as one line

## What happens

An element's head comment is written after the `- ` marker. Every line of it was written
without a break between them, so

    - # one
      # two
      1

came out as `- # one# two` and re-read as ONE comment line. A second pass had nothing left to
separate, so the loss is silent and permanent: the document is stable after it, and stable on
the wrong shape.

## The fix

Each line after the first starts a line of its own, indented to where the value goes
(`writeElementHeadComment`, encode.go), which is what the reader expects and what a second pass
reproduces.

Covered by two cases in `TestElementHeadComment`: a comment of more than one line on the first
element, and one on a later element, where the marker and the indent both matter.