# Syntax Highlighting for Tony in Markdown

This document explains how to get Tony-specific syntax highlighting in markdown
code blocks for public consumption.

## Quick Start

### Option 1: Pygments (Recommended for most tools)

Install the Pygments lexer:

```bash
pip install -e docs/pygments-tony
```

Then use `tony` code blocks in your markdown:

````markdown
```tony
!tag
key: value
<<: merge
```
````

See [docs/pygments-tony/README.md](pygments-tony/README.md) for detailed integration instructions.

### Option 2: TextMate Grammar (For VS Code, GitHub, etc.)

We provide a TextMate grammar at `docs/editors/tony.tmLanguage.json` that works with:
- **VS Code**: Already configured in this repo
- **GitHub**: Can be used via Linguist (see below)
- **Other editors**: Any editor supporting TextMate grammars

## Platform-Specific Instructions

### GitHub / GitLab

**For markdown code blocks**: GitHub uses a syntax highlighter that doesn't easily support custom languages. Options:

1. **Use Pygments via GitHub Pages**: If hosting docs on GitHub Pages with Jekyll, configure Pygments (see Jekyll section below)

2. **Use Rouge with TextMate grammar**: Rouge (used by Jekyll) can use TextMate grammars. Convert or use Rouge's TextMate support.

3. **Fallback**: Unfortunately, GitHub's markdown renderer doesn't support custom languages in code blocks. You may need to use `yaml` as a fallback, or host docs elsewhere.

**For `.tony` files**: GitHub uses Linguist for file detection. You can add a `.gitattributes` file:

```
*.tony linguist-language=YAML
```

This makes `.tony` files render with YAML highlighting (not ideal, but better than nothing).

### VS Code

VS Code already supports Tony syntax highlighting via the TextMate grammar in
`docs/editors/tony.tmLanguage.json`.

For markdown preview, configure in `.vscode/settings.json`:

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
pip install -e docs/pygments-tony
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
bundle exec pip install -e docs/pygments-tony
```

**Option 2: Use Rouge with TextMate grammar**

Rouge can use TextMate grammars. You'll need to convert `tony.tmLanguage.json`
to Rouge format or use a converter tool.

### Pelican

Pelican uses Pygments by default. Install the lexer:

```bash
pip install -e docs/pygments-tony
```

### Docusaurus

Docusaurus uses Prism.js or Shiki. You would need to:
1. Create a Prism.js language definition, or
2. Use Shiki with a custom TextMate grammar

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

To improve the lexer, edit `docs/pygments-tony/pygments_tony.py` and test with the code above.

## See Also

- [TextMate Grammar](editors/tony.tmLanguage.json) - For editor support
- [Pygments Lexer](pygments-tony/) - For markdown/documentation tools
