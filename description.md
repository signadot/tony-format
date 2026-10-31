# a document that is only a tag is refused; Implicit Null names two places

docs/tony.md, Implicit Null (e46b7865), lists a document that is only a tag, `!t`, as a third place a null may go unwritten. The parser reads it as `!t null`. The wire decoder reads it as the end of the stream, with no document and no error. The shape has no use case, so Scott decided (2026-10-31) that it is refused rather than made to work on the wire.

Fix:
- docs/tony.md: Implicit Null lists two places, and `!t` alone joins the invalid examples.
- token: in tony mode, a tag followed only by comments and the end of the document is refused. YAML mode is unchanged.
- stream: a stream that ends with a tag pending is an error, not EOF.

A tag whose value starts on the next line, such as `!eval` over an object, is unaffected.