"""
Pygments lexer for Tony syntax highlighting.

Installation:
    pip install -e .
    
Usage in Python:
    from pygments import highlight
    from pygments.formatters import HtmlFormatter
    from pygments_tony import TonyLexer
    
    code = '''!tag
    key: value
    <<: merge
    '''
    print(highlight(code, TonyLexer(), HtmlFormatter()))

Usage in Markdown:
    ```tony
    !tag
    key: value
    <<: merge
    ```
"""

from pygments.lexer import RegexLexer, bygroups, include
from pygments.token import (
    Text, Comment, Keyword, Name, String, Number, 
    Punctuation, Operator, Literal
)


class TonyLexer(RegexLexer):
    """
    Lexer for Tony (a YAML dialect).
    
    Tony-specific features:
    - Tags: !tag, !tag.subtag, !tag(args)
    - Merge keys: <<:
    - String interpolation: $[var]
    - Node replacement: .[path]
    """
    name = 'Tony'
    aliases = ['tony']
    filenames = ['*.tony']
    mimetypes = ['text/x-tony']
    
    tokens = {
        'root': [
            # Document separator
            (r'^---\s*$', Comment.Special),
            
            # Comments (with TODO/FIXME highlighting)
            (r'#.*?(TODO|FIXME|XXX|NOTE).*$', Comment.Special),
            (r'#.*$', Comment),
            
            # Merge keys (must be at start of line with optional whitespace)
            (r'^\s*<<:\s*', Operator),
            
            # Tags: !tag, !tag.subtag, !tag(args) - matches TextMate grammar pattern
            # Allows more characters: a-zA-Z0-9_\-.:/+=~@$%^&*
            # Note: - must be in the middle or escaped, not at start/end of character class
            (r'!([a-zA-Z0-9_.:/+=~@$%^\-&]+(?:\.[a-zA-Z0-9_.:/+=~@$%^\-&]+)*)'
             r'(?=\s|:|$|\[|\{|\(|,)', Keyword),
            
            # Tags with arguments: !tag(...)
            (r'!([a-zA-Z0-9_.:/+=~@$%^&-]+(?:\.[a-zA-Z0-9_.:/+=~@$%^&\-]+)*)\(',
             bygroups(Keyword), 'tag-args'),
            
            # String interpolation: $[var] (inside strings)
            (r'\$\[[^\]]+\]', Name.Variable),
            
            # Node replacement: .[path]
            (r'\.\[[^\]]+\]', Name.Variable),
            
            # Block literals: |, |-, |+ (at start of line)
            (r'^(\s*)(\|[\+\-]?)\s*$', bygroups(Text, Punctuation), 'block-literal'),
            
            # Quoted strings (double) with interpolation
            (r'"', String.Double, 'double-string'),
            
            # Quoted strings (single)
            (r"'([^'\\]|\\.)*'", String.Single),
            
            # Numbers
            (r'-?\d+\.\d+', Number.Float),
            (r'-?\d+', Number.Integer),
            (r'-?0[xX][0-9a-fA-F]+', Number.Hex),
            (r'-?0[oO][0-7]+', Number.Oct),
            
            # Booleans and null
            (r'\b(true|false|null)\b', Keyword.Constant),
            
            # Punctuation
            (r'[\[\]{}]', Punctuation),
            (r'[:,-]\s+', Punctuation),
            
            # Whitespace
            (r'\s+', Text),
            
            # Everything else (keys, values)
            (r'[^\s\[\]{}:,!$.#"\'-]+', Name),
        ],
        'tag-args': [
            # Tag arguments can contain strings, numbers, booleans, null, literals
            (r'"', String.Double, 'double-string'),
            (r"'([^'\\]|\\.)*'", String.Single),
            (r'-?\d+\.\d+', Number.Float),
            (r'-?\d+', Number.Integer),
            (r'\b(true|false|null)\b', Keyword.Constant),
            (r'[a-zA-Z0-9_.:/+=~@$%^&*-]+', Name),
            (r',', Punctuation),
            (r'\)', Keyword, '#pop'),
            (r'\s+', Text),
        ],
        'double-string': [
            (r'\\[\\"nrtbf]', String.Escape),
            (r'\$\[[^\]]+\]', Name.Variable),  # Interpolation
            (r'"', String.Double, '#pop'),
            (r'[^\\"$]+', String.Double),
        ],
        'block-literal': [
            # Block literal content - continue until indentation changes
            (r'^(\s+).*$', String),
            (r'^[^\s]', Text, '#pop'),  # Non-indented line ends block
        ],
    }
