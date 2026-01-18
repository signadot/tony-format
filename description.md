# .[...] eval syntax needs to permit escaping ]

consider .[a.b["c-foo"]] or .["[" + a + "]"]

We need a solid escaping mechanism to allow inputs like this.

I'd propose backslash escaping \] -> ] and \\ -> \