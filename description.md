# o docs: every generated page under docs/o/ links to itself as "published documentation"

Each page under docs/o/ ended with a See also entry, "published documentation",
pointing at https://signadot.github.io/tony-format/o/<page>/ -- which is the page
itself, since mkdocs publishes docs/o/ as it is. On the root page it was the only
See also entry, so the section existed to link to itself.

The link is a cli.Doc feature for a markdown copy that lives somewhere other than
the site. cmd/o/docs.go had a Page mapper (sitePage) deriving the URL from the file
name so that it at least resolved; it now passes a mapper answering "" for every
command, which cli.Doc takes as "no page on the site" and writes nothing. Site stays
set, since the term links (logd, docd, schema, IR) hang off it.

docs/o/ regenerated: 27 pages lose the entry, the root page loses See also.