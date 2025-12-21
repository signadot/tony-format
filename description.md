# schema validation

Tony schema lack validation for tony documents.  This issue tracks its implementation

The trickiest part of this will probably the type parameters, as tony supports parameters
schema and definitions within schema.

This issue should probably design the algorithm for expanding parameters like in
`!array(int)` first.

