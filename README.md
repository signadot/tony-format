# Tony Format

[![Go Reference](https://pkg.go.dev/badge/github.com/signadot/tony-format/go-tony.svg)](https://pkg.go.dev/github.com/signadot/tony-format/go-tony)

A data format where patches, queries, and schemas are expressed as documents themselves — combining JSON's structure with YAML's readability while preserving comments and metadata.

- [Documentation](https://signadot.github.io/tony-format/)
- [Why Tony? Comparisons with Kustomize, Helm, and others](docs/comparison.md)
- [Go](go-tony/README.md)

## Status

The core Tony format is fairly stable and fun.  One CLI tool with several
basic commands, [`o`](go-tony/README.md), is in use and fairly stable.

**Relatively Stable and usable:**
- Schema
- Mappings to Go
- Go CodeGen

**In progress:**
- System API
- LSP

## Contributing

The Tony format is open source.

As a naisant, enhanced data format for an interconnected and increasingly automated world,
a lot of possibilities, some substantial, exist.

This project uses [`git issue`](https://pkg.go.dev/github.com/signadot/tony-format/go-tony/cmd/git-issue#section-readme)
for issue tracking — a git-native tracker that stores issues directly in the
repository as git refs. Install it and run `git issue list` to see open issues.

Feel free to reach out using `git issue` or Discussions, give the tools a try, weigh in on
the direction of designs, or let us know how you'd like to see the format and tooling
governed.
 
