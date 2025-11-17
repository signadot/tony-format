
## Extra-Format Encoding Capabilities

To facilitate generation of text outside of an object from within, for example
to generate helm charts, Tony provides a special, non-YAML but very YAML-ish
facility.  The [merge key syntax](https://yaml.org/type/merge.html) is part of
YAML 1.1 and basically deprecated.  But, it reads nicely for outputing raw text.

```tony
# input document
<<: |
  output this as raw text
<<: |
  and this too
---
# output with -x
output this as raw text
and this too
```

This was originally used for merging maps via YAML anchors, and the usage here
is illegal yaml because it uses strings instead of maps as values for `<<`
merge keys.  We think this deviation from YAML is fine b/c its purpose is
indeed to produce non-yaml.

This permits adding helm chart range loops in annotations, adding headers and
footers for conditional inclusion in Helm, etc.

Although it is not YAML per se, parsers are likely to accept it if run allowing
duplicate keys.  When marshalling input to JSON, such as when using json-patch
or evaluating the environment, these merge keys are elided.

### Merge Key Indentation

When merge keys are encoded with `-x`, they come out at the indentation level
of keys in the object/mapping to which they belong:

```tony
a:
  b:
    c: true
    <<: stuff
    d: false
  e: z
  <<: zoo
<<: thwart
d: e
---
a:
  b:
    c: true
    stuff
    d: false
  e: z
  zoo
thwart
d: e
```

### Document Wrapping Example

With Tony, one can use this mechanism in patches to wrap documents:

```tony
# input document
name: "Friendly's"
number: 1
---
# patch
patch:
  !embed(X)
  <<: |
    place this before
  <<: X
  <<: |
    random
    trailing
    stuff
---
# result
place this before
name: "Friendly's"
number: 1
random
trailing
stuff
```
