# Pygments Tony Lexer

Pygments syntax highlighting lexer for
[Tony](https://github.com/signadot/hackspace/yt) code blocks in markdown.

## Installation

The docs build installs this from the repository root, along with everything
else the site needs:

```bash
pip install -r requirements.txt
```

To work on the lexer, install it editable so that a change takes effect
without reinstalling -- a plain install copies the module into site-packages,
and a local `mkdocs build` then highlights with the copy rather than with the
file you are editing:

```bash
pip install -e docs/sketchy/pygments-tony
```

## Usage

### In Python

```python
from pygments import highlight
from pygments.formatters import HtmlFormatter, TerminalFormatter
from pygments_tony import TonyLexer

code = '''!tag
key: value
<<: merge
'''

# HTML output
print(highlight(code, TonyLexer(), HtmlFormatter()))

# Terminal output  
print(highlight(code, TonyLexer(), TerminalFormatter()))
```

### In Markdown

Once installed, Pygments will automatically recognize `tony` code blocks:

````markdown
```tony
!tag
key: value
<<: merge
```
````

## Integration with Documentation Tools

### MkDocs

MkDocs uses Pygments by default. After installing this package, `tony` code blocks will be highlighted automatically.

### Sphinx

Add to your `conf.py`:

```python
from pygments_tony import TonyLexer

def setup(app):
    app.add_lexer('tony', TonyLexer())
```

### Jekyll / GitHub Pages

Jekyll uses Rouge by default. For Pygments support, configure Jekyll to use Pygments in `_config.yml`:

```yaml
markdown: kramdown
kramdown:
  syntax_highlighter: pygments
```

Then install this package in your Jekyll environment.

### Pelican

Pelican uses Pygments by default. Install this package and `tony` code blocks will work automatically.

## Features

The lexer highlights:
- **Tags**: `!tag`, `!tag.subtag`, `!tag(args)`, including arguments that are
  themselves tags
- **Merge keys**: `<<:`
- **Keys**, told from values by the `:` separator, and sparse-array keys from
  object keys
- **Literals** holding the punctuation Tony allows in them: `a:b`, `.[x]`,
  `$y`, `no-delegate`, `tony-format/context`
- **Digit-leading strings**: `100m`, `1.2.3` are strings, not `100` and `1.2`
- **Block literals**: `|`, `|-`, `|+`, and the lines indented past them
- **Comments**: `# comment`, which need no preceding space
- **Strings, numbers** in every notation, **booleans, null**

Each construct is given the token whose CSS class carries the colour `o v`
prints it in; `docs/pygments.css` holds the other half of that mapping.

## Tests

```bash
python test_lexer.py
```

The last test lexes every ```tony block in the docs and fails on any character
the lexer has no rule for.

## License

MIT
