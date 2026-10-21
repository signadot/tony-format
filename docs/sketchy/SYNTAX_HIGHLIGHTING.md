# Syntax Highlighting for Tony in Markdown

This document explains how to get Tony-specific syntax highlighting in markdown
code blocks for public consumption.

## Quick Start

### Option 1: Pygments (Recommended for most tools)

Install the Pygments lexer:

```bash
pip install -e docs/sketchy/pygments-tony
```

Then use `tony` code blocks in your markdown:

````markdown
```tony
!tag
key: value
<<: merge
```
````

See [the lexer README](pygments-tony/README.md) for detailed integration instructions.

### Option 2: Tree-sitter grammar (for editors that use one)

A tree-sitter grammar lives at
[`docs/sketchy/editors/tree-sitter-tony`](editors/tree-sitter-tony/grammar.js), for
editors and tools that take one — Neovim, Helix, Zed, and anything else built on
tree-sitter.

### Option 3: TextMate grammar (linguist, Shiki, Sublime, VS Code)

A TextMate grammar lives at [`grammar/`](https://github.com/signadot/tony-format/tree/main/grammar) at the root of the repository, tested
against the Pygments lexer's cases so that the two highlight alike. It is the grammar a
linguist PR would add for GitHub, and it is not packaged for any editor: the platforms below
that read TextMate grammars can load it, and the note for each says what that takes.

## Platform-Specific Instructions

### GitHub / GitLab

**For markdown code blocks**: GitHub uses a syntax highlighter that doesn't easily support custom languages. Options:

1. **Use Pygments via GitHub Pages**: If hosting docs on GitHub Pages with Jekyll, configure Pygments (see Jekyll section below)

2. **Rouge**: Jekyll highlights with Rouge, whose lexers are hand-written Ruby classes. It cannot load a TextMate grammar, so [the one in `grammar/`](https://github.com/signadot/tony-format/tree/main/grammar) is no help there: Tony would need a Rouge lexer written for it, and GitHub Pages' own Jekyll accepts no custom lexers anyway.

3. **Fallback**: Unfortunately, GitHub's markdown renderer doesn't support custom languages in code blocks. You may need to use `yaml` as a fallback, or host docs elsewhere.

**For `.tony` files**: GitHub uses Linguist for file detection. You can add a `.gitattributes` file:

```
*.tony linguist-language=YAML
```

This makes `.tony` files render with YAML highlighting (not ideal, but better than nothing).

### VS Code

VS Code highlights by TextMate grammar. [The one in `grammar/`](https://github.com/signadot/tony-format/tree/main/grammar) is not packaged as
an extension, so `.tony` files are unhighlighted until it is: that takes a `package.json`
manifest that contributes the `tony` language and the grammar's path.

For markdown preview, the usual fallback is to configure in `.vscode/settings.json`:

```json
{
  "markdown.extension.codeBlockLanguages": [
    "tony:yaml"  // Falls back to YAML if Tony not recognized
  ]
}
```

Or install a markdown extension that supports custom lexers.

### MkDocs

MkDocs uses Pygments by default. Install the lexer:

```bash
pip install -e docs/sketchy/pygments-tony
```

Then `tony` code blocks will work automatically.

### Sphinx

Add to `conf.py`:

```python
from pygments_tony import TonyLexer

def setup(app):
    app.add_lexer('tony', TonyLexer())
```

### Jekyll / GitHub Pages

**Option 1: Use Pygments**

In `_config.yml`:

```yaml
markdown: kramdown
kramdown:
  syntax_highlighter: pygments
```

Then install the lexer in your Jekyll environment:

```bash
bundle exec pip install -e docs/sketchy/pygments-tony
```

**Option 2: Rouge**

Rouge's lexers are hand-written Ruby classes, and it has no way to load or convert a TextMate
grammar, so Tony would need a Rouge lexer written for it; none exists. GitHub Pages' own Jekyll
accepts no custom lexers, so one would only serve a Jekyll built elsewhere.

### Pelican

Pelican uses Pygments by default. Install the lexer:

```bash
pip install -e docs/sketchy/pygments-tony
```

### Docusaurus

Docusaurus uses Prism.js or Shiki. You would need to:
1. Create a Prism.js language definition, or
2. Load [the TextMate grammar](https://github.com/signadot/tony-format/tree/main/grammar/syntaxes/tony.tmLanguage.json) into Shiki

### Hugo

Hugo uses Chroma (a Go port of Pygments). You would need to create a Chroma
lexer or use a TextMate grammar converter.

## Testing

Test the Pygments lexer:

```python
from pygments import highlight
from pygments.formatters import TerminalFormatter
from pygments_tony import TonyLexer

code = '''!tag
key: value
<<: merge
$[var]
.[path]
'''

print(highlight(code, TonyLexer(), TerminalFormatter()))
```

## Contributing

To improve the lexer, edit `docs/sketchy/pygments-tony/pygments_tony.py` and test with the code above.

## See Also

- [Tree-sitter grammar](editors/tree-sitter-tony/grammar.js) — for editors built on tree-sitter
- [TextMate grammar](https://github.com/signadot/tony-format/tree/main/grammar) — for linguist, Shiki, and anything else that reads one
- [Pygments lexer](pygments-tony/README.md) — for markdown and documentation tools
