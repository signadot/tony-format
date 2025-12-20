# logd: support indexing !key indexed arrays

logd needs to support efficient indexed lookup for !key indexed schema

Technnically, !key takes an argument of a ir path and then the byte representation
of the value at that path becomes the key.

Not sure how much harder that is to do than the non-generic !key(field) where field is a string,
but sure would be nice.

This needs a design first which coordinates storage index and dlog entries and an implementation
plan w.r.t. changes for kpaths.


