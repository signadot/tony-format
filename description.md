# --title Comment association panic in parser

When parsing a build.tony file with comments, there's a panic in `associateComments` when accessing an empty `Values` array.

**Steps to reproduce:**
Parse a build.tony file containing YAML comments (lines starting with #).

**Error:**
Panic at line 27 in comment association code accessing empty `Values` array.

**Workaround:**
Remove comments from the build.tony file temporarily.