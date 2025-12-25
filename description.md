# logd scopes

This issue is to scope out how we may have logd COW scopes,
which would be nice for interop with signadot sandboxes.

rough idea:

sessions can specify scopeid

each scope inherits from default scope for reads and
writes go to scope-specific paths mirroring the baseline

rough sketch:
logentry has new scope-id field and comparisons reference it
for writes and reads read both with and with-out scope-id

operations:

create-scope:
delete-scope:
match/patch/watch with optional scope

perhaps merges of scopes to default can just be manual, as anyway
the data is likely to be throw-away


This item relates to logd having access to schema, as scopes are
a natural playground for schema changes