# logd: a read at a past commit follows today's schema, so across a keying change it answers the store's object of names, or refuses the element path

xnepz3sfh12ksn5qn9n0 fixed the set walk's enumeration; two more places on the read path still ask the schema in force NOW (SchemaFor) where the commit read has its own (SchemaForAt):

- `raises()` picks the encoded fast path when today's schema keys nothing, so a body that was a keyed array at the commit read is streamed unraised.
- `ident.CanonicalPath` spells an element path by today's schema, in handleMatch and in the walk's canonicalChild.

Measured, after `runs` loses its identity (SetSchema force) and a read at the commit before it:

- `{match: {path: runs, commit: N}}` answers `{(id=r1): {...}, (id=r2): {...}}` -- the store's object of names, not the array a read at N raises it to.
- `{match: {path: "runs(r1)", commit: N}}` answers invalid_path: "runs has no identity, so (r1) names no element of it".

Fix: the read path asks the schema at the commit it reads, as raising and the walk now do.