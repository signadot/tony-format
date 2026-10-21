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
test says so. The suite also tokenizes every ` ```tony ` block and every `.tony` file in the
repository, and fails on any character the grammar gives no scope a theme colours
-- none but the root's, or only meta scopes, which group without colouring. `npm test` fails
as well if the JSON is not what `build.mjs` builds.

A block literal is recognised on an opening line of up to eight `- ` markers (`MAX_DEPTH` in
`build.mjs`): a TextMate end pattern cannot count, so each depth is a rule of its own. One
deeper is painted as plain literals. The tests cover every depth to the cap, and nothing in
this repository goes past two.

## GitHub

GitHub highlights a file only in a language linguist knows, and a repository cannot bring its
own grammar. Until Tony is added there, repositories map it to YAML in `.gitattributes`:

```
*.tony linguist-language=YAML
```

Adding Tony to linguist takes no code, there or here. The PR is data: an entry in
`lib/linguist/languages.yml`, this grammar added as a submodule by `script/add-grammar`, a few
real `.tony` files under `samples/Tony/`, and a language id from `script/update-ids`. No
language in linguist claims `.tony` today, but a different language does use it, and by the
same name: "Tony", the language of the NTUA compilers course, has many public `.tony` programs
(`kostis/ntua_compilers`, `kandrio/Tony-Compiler`). linguist would classify those as this
format, so the PR has to answer for it, with a distinct name, a heuristic, or both. And it needs
use: linguist takes a language once its extension is in wide public use -- some 2,000 `.tony`
files across many owners' repositories -- and today almost all of them are this repository's.

Once it merges, GitHub highlights `.tony` as Tony from the next linguist release it deploys,
and the `.gitattributes` mapping can go.

## Status

What a linguist PR needs, and where each stands.

- [x] The grammar, `syntaxes/tony.tmLanguage.json`, generated from commented source
- [x] Tests: the Pygments lexer's cases, and every `.tony` file and ```` ```tony ```` block in
      the repository
- [x] Tests run on every PR that touches what they read -- `grammar/`, any `.md` or `.tony`
      file -- and on pushes to main that do (`.github/workflows/grammar.yml`)
- [x] A license linguist accepts: Apache-2.0, the repository's
- [x] In a public repository linguist can add as a submodule
- [x] Compiles with linguist's grammar compiler (`script/grammar-compiler add`): no errors, no
      warnings
- [x] The PR, staged against a clone of linguist, passes its sample, classifier, heuristic,
      ordering and id tests (its language tests need the submodules a shallow clone lacks): a
      `Tony` entry (`type: data`,
      `tm_scope: source.tony`, YAML's ace and CodeMirror modes), and three samples from this
      repository, all Apache-2.0: `go-tony/schema/base.tony`,
      `go-tony/examples/build/sb/patches/workloads.tony` and
      `go-tony/examples/build/patch-argocd/source/plugin-config.tony`
- [x] A colour: `#4a5c8a`, the colour `o v` prints a tag in
- [ ] Usage: ~2,000 public `.tony` files across many owners (today almost all are this
      repository's; signadot's other `.tony` files are in private repositories)
- [ ] A name or heuristic that keeps the NTUA course's `.tony` programs from being classified as
      this format
- [ ] Snapshot tests, so a grammar change reads as a diff of scopes in review

Not planned until someone asks: packaging for VS Code or Cursor, which takes a
`package.json` manifest and a language configuration around this grammar, and for Zed, which
reads tree-sitter grammars rather than TextMate ones.
