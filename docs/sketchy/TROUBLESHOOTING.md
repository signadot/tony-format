# Troubleshooting Tony Syntax Highlighting in MkDocs

## Issue: MkDocs not coloring Tony code blocks after `pip install`

### Step 1: Verify Pygments is installed

MkDocs uses Pygments for syntax highlighting. Make sure Pygments is installed:

```bash
pip install pygments
```

### Step 2: Verify the lexer is installed correctly

Check if the Tony lexer is registered:

```bash
python -c "from pygments import lexers; print('tony' in [x[0] for x in lexers.get_all_lexers()])"
```

Should output: `True`

Or test directly:

```bash
python -c "from pygments import lexers; lexer = lexers.get_lexer_by_name('tony'); print(lexer.name)"
```

Should output: `Tony`

### Step 3: Check Python environment

**Important**: Make sure you installed the package in the **same Python environment** that MkDocs uses.

Check which Python MkDocs uses:

```bash
mkdocs --version
which python  # or which python3
```

Install in that environment:

```bash
# If using a virtualenv
source venv/bin/activate  # or your venv path
pip install -e docs/pygments-tony

# Or install globally
pip install -e docs/pygments-tony
```

### Step 4: Enable codehilite in MkDocs

Make sure `codehilite` extension is enabled in `mkdocs.yml`:

```yaml
markdown_extensions:
  - codehilite:
      use_pygments: true
  - smarty: {}
```

### Step 5: Restart MkDocs server

After installing, restart the MkDocs dev server:

```bash
# Stop the server (Ctrl+C)
mkdocs serve  # Start again
```

### Step 6: Verify the code block syntax

Make sure you're using the correct syntax:

````markdown
```tony
!tag
key: value
```
````

Not:
````markdown
```yaml
!tag
key: value
```
````

### Step 7: Check for caching

Clear any caches:

```bash
rm -rf site/  # MkDocs output directory
mkdocs build  # Rebuild
```

### Step 8: Debug with Python

Test if Pygments can highlight Tony code:

```python
from pygments import highlight
from pygments.formatters import HtmlFormatter
from pygments import lexers

# Get the lexer
lexer = lexers.get_lexer_by_name('tony')
code = '''!tag
key: value
<<: merge
'''

# Format
formatter = HtmlFormatter()
result = highlight(code, lexer, formatter)
print(result)
```

If this works but MkDocs doesn't, it's likely an environment or configuration issue.

### Common Issues

1. **Multiple Python environments**: MkDocs might be using a different Python than where you installed the package
2. **Virtualenv not activated**: Make sure your virtualenv is activated when running MkDocs
3. **Entry point not registered**: After installing, Pygments needs to discover the entry point. Try:
   ```bash
   python -c "import pygments.lexers; pygments.lexers._lexer_cache.clear()"
   ```
4. **MkDocs cache**: Clear the site directory and rebuild

### Still not working?

Check MkDocs logs for errors:

```bash
mkdocs serve --verbose
```

Look for any errors related to Pygments or code highlighting.
