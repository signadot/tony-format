# Tony grammar

A TextMate grammar for [Tony](../docs/tony.md): `syntaxes/tony.tmLanguage.json`.

TextMate grammars are what GitHub's [linguist](https://github.com/github-linguist/linguist)
highlights with, and what VS Code, Sublime Text and Shiki read. This one exists so that
`.tony` files can be highlighted as Tony on GitHub; it is not packaged for any editor.

## Changing it

The JSON is generated: edit `build.mjs`, where the regexes are assembled from named and
commented parts, and run it.

```bash
cd grammar
npm install
node build.mjs
npm test
```

The grammar and [the Pygments lexer](../docs/sketchy/pygments-tony/pygments_tony.py) that
highlights the docs site are two accounts of one syntax, and `test/grammar.test.mjs` is a port
of the lexer's tests, so a change to one belongs in the other. Where they differ on purpose, a
test says so. The suite also tokenizes every ` ```tony ` block in the docs and every `.tony`
file in the repository, and fails on any character the grammar gives no scope. `npm test`
fails as well if the JSON is not what `build.mjs` builds.

## GitHub

GitHub highlights a file only in a language linguist knows, and a repository cannot bring its
own grammar. Until Tony is added there, repositories map it to YAML in `.gitattributes`:

```
*.tony linguist-language=YAML
```

Adding Tony to linguist is a PR there that points `script/add-grammar` at this repository and
adds real-world samples. linguist accepts a language once its extension is in wide public use
-- some 2,000 `.tony` files across many owners' repositories -- and `.tony` is also the
extension of an unrelated teaching language, which the PR would need a heuristic to tell apart.

## Status

What a linguist PR needs, and where each stands.

- [x] The grammar, `syntaxes/tony.tmLanguage.json`, generated from commented source
- [x] Tests: the Pygments lexer's cases, and every `.tony` file and ```` ```tony ```` block in
      the repository
- [x] Tests run on every PR (`.github/workflows/grammar.yml`)
- [x] A license linguist accepts: Apache-2.0, the repository's
- [x] In a public repository linguist can add as a submodule
- [ ] Snapshot tests, so a grammar change reads as a diff of scopes in review
- [ ] Samples for linguist: real `.tony` files, licensed for the PR
- [ ] Usage: ~2,000 public `.tony` files across many owners (today almost all are signadot's)
- [ ] A heuristic telling Tony from the NTUA teaching language that also uses `.tony`

Not planned until someone asks: packaging for VS Code or Cursor, which takes a
`package.json` manifest and a language configuration around this grammar, and for Zed, which
reads tree-sitter grammars rather than TextMate ones.
