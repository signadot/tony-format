# Operations Reference

This page indexes the reference for Tony operations.

## Where each thing lives

- **The list, from the binary**: `o match -tags` and `o patch -tags` print the
  operations the build actually registers. That is the authority.
- **The one-line table**: [matchpatch.md](./matchpatch.md), kept equal to the
  registry by a test.
- **The per-operation pages** below, which carry prose and examples. A Symbol
  knows its name and whether it matches or patches and nothing more, so these
  cannot be generated from the registry; they are written by hand, and a test
  (`mergeop.TestReferenceCoversEveryOperation`) fails when an operation is added
  without an entry.

## Operation Categories

- **[Mergeop Operations](./generated/mergeop.md)** - Operations for matching and patching documents
- **[Eval Operations](./generated/eval.md)** - Operations for evaluating and transforming values

## Updating Documentation

Add or change an operation, then edit `docs/generated/mergeop.md` (or
`docs/generated/eval.md`) and the table in [matchpatch.md](./matchpatch.md).
Both are checked by tests in the `mergeop` package, which say what is missing.

This page used to describe a `docgen` tool writing these files from
`mergeop/doc.go`. There is no such command in the repository and those files are
package documentation, not per-operation entries -- so the instructions could not
be followed, and the reference drifted to covering 10 of 33 operations before
anyone noticed. The directory is still called `generated` for its URLs.

## Integration with MkDocs

Add the generated documentation to `mkdocs.yml`:

```yaml
nav:
  - Reference:
    - operations.md
    - generated/mergeop.md
    - generated/eval.md
```
