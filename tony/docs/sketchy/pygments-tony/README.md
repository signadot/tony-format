# Pygments Tony Lexer

Pygments syntax highlighting lexer for
[Tony](https://github.com/signadot/hackspace/yt) code blocks in markdown.

## Installation

```bash
pip install -e docs/pygments-tony
```

Or if published to PyPI:
```bash
pip install pygments-tony
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
$[var]
.[path]
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
- **Tags**: `!tag`, `!tag.subtag`, `!tag(args)`
- **Merge keys**: `<<:`
- **String interpolation**: `$[var]`
- **Node replacement**: `.[path]`
- **Block literals**: `|`, `|-`, `|+`
- **Comments**: `# comment`
- **Strings, numbers, booleans, null**

## License

MIT
