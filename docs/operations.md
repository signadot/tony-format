# Operations Reference

This page provides a reference for all Tony operations. The documentation is generated from the operation registry using `docgen`.

## How to Use This Documentation

- **CLI Help**: Run `o help <category> <op>` for quick reference
- **Web Docs**: See the generated pages below for detailed documentation
- **Source**: Documentation is maintained in `mergeop/doc.go` and `eval/doc.go`

## Operation Categories

- **[Mergeop Operations](./generated/mergeop.md)** - Operations for matching and patching documents
- **[Eval Operations](./generated/eval.md)** - Operations for evaluating and transforming values

## Updating Documentation

To update operation documentation:

1. Edit the appropriate `doc.go` file:
   - `mergeop/doc.go` for mergeop operations
   - `eval/doc.go` for eval operations

2. Regenerate the documentation:
   ```bash
   docgen docs/generated
   ```

3. The generated markdown files will be updated automatically.

## Integration with MkDocs

Add the generated documentation to `mkdocs.yml`:

```yaml
nav:
  - Reference:
    - operations.md
    - generated/mergeop.md
    - generated/eval.md
```
