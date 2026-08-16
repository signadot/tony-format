# gomap: FieldInfo's comment fields are declared but never populated, so a field-level comment= is silently ignored

`gomap.FieldInfo` declares

    // CommentFieldName is the name of a struct field (type []string) to populate with comment data
    CommentFieldName string
    // LineCommentFieldName is the name of a struct field (type []string) to populate with line comment data
    LineCommentFieldName string

and nothing ever assigns them. Every assignment site — gomap/tags.go:394, codegen/parser.go:245
and :538 — is building a StructSchema, not a FieldInfo. So the per-field capability the type
describes does not exist, and a field tag asking for it is accepted and does nothing:

    GatesDoc []string `tony:"-"`
    Gates    []Gate   `tony:"field=gates,omitzero,comment=GatesDoc"`

generates a codec with no mention of GatesDoc. No error, no warning.

WHY IT MATTERS. The struct-level `comment=` captures the comment on a struct's OWN node, which
covers a comment above a value that decodes into a struct. It cannot cover a comment above a
field whose value is a LIST or a scalar, because there is no struct there to carry it — and the
per-field hook is exactly the thing that would.

Concretely, in a verse charter:

    - name: review
      # the guard          <- above a list-valued field: nowhere to live
      gates:
      - { name: approval, mode: ask }
      # the consequence    <- above a struct-valued field: kept, via Action's comment=
      action:
        patch: { stage: reviewing }

One of those two survives a decode/re-encode and the other does not, and the difference is not
one an author can see. That is the shape worth avoiding: half a document's comments coming back
looks like it worked.

Two things would each be an improvement on their own, and the second is cheap:

1. Populate FieldInfo.CommentFieldName/LineCommentFieldName from the field tag, and honour them
   in the generated and reflected codecs — the wrapper is on the field's value, so it is the
   same capture and re-attach the struct-level one already does.
2. Failing that, REFUSE a field-level `comment=`/`lineComment=` at codegen rather than ignoring
   it, so a struct asking for something unsupported says so instead of quietly not doing it.