# logd: depth is one budget for the whole path, counted from the node the path names

depth bounds each `..` on its own (zpx2x6b0h12ks05andn0), so a.b..c..d at depth 1 reaches d two to four segments below a.b, and at depth n with k descents the answer can lie k×n segments off the literal path. A request parameter should be one budget: how far the walk may stray from what the path spells, counted from the node the path names.

So a depth is shared by every `..` in the path: a.b..c..d at depth 1 is a.b.c.d, a.b.X.c.d and a.b.c.Y.d, not a.b.X.c.Y.d. With one `..`, which is nearly every query, nothing changes: a.b.c.. at depth 1 is a.b.c and its children, and a.b.c.d.. at depth 1 is the same one level down -- ls at any level, with the depth never moving. Counting from the root instead, as verse's CLI does, is what makes ls at the next level two edits, and is what this avoids.

The count carries across a descent rather than starting over at the next one, in kpath.Positions and in docd's mount check. Nothing on the wire changes.