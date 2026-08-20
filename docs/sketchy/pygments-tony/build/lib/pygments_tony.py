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

The token each construct is given is the one whose CSS class carries the
colour `o v` prints it in, so a document reads the same on the site as it does
in the terminal.  docs/pygments.css holds the other half of that mapping.

    construct              token                colour
    tag                    Keyword              RGB(74,92,138)
    comment                Comment              blue
    object key             Name.Attribute       RGB(128,168,196)
    sparse-array key       Name.Label           RGB(196,96,16)
    merge key '<<'         Name.Builtin         RGB(196,168,128)
    ':'                    Operator             RGB(196,128,128)
    '-', ',', brackets     Punctuation          RGB(255,0,196)
    literal                String.Other         RGB(88,158,86)
    quoted string          String.Single/Double RGB(8,196,16)
    block literal          String.Heredoc       RGB(198,198,46)
    number                 Number               RGB(128,216,236)
    null                   Keyword.Constant     RGB(168,0,196)
    true, false            Literal              cyan

`o v` leaves brackets uncoloured; here they are separators like ',' and '-'.
"""

from pygments.lexer import RegexLexer, bygroups, this, using
from pygments.token import (
    Comment, Keyword, Literal, Name, Number, Operator, Punctuation, String,
    Text
)


# A literal is a maximal run of non-whitespace characters, as defined in
# "Literals and Punctuation" in docs/tony.md.  Whitespace ends it, and so does
# any punctuation outside the set Tony allows -- of which ',' '#' and the quote
# characters are the ones that turn up in practice.
_LIT_CH = r'[^\s,#\'"\[\]{}():]'

# ':' is part of the literal ("a:b", "resolvers:security:review") unless it is
# the key separator, which is what a following space, ',' or end of line makes
# it.
_LIT_COLON = r':(?![\s,]|$)'

# The paired characters belong to a literal when they are balanced: ".[x]",
# "$[y]", "f(a)".  Nesting deeper than this is not worth a regex.
_LIT_PAIR = r'\[[^\[\]\s]*\]|\{[^{}\s]*\}|\([^()\s]*\)'

_LIT_BODY = r'(?:%s|%s|%s)' % (_LIT_CH, _LIT_COLON, _LIT_PAIR)
_LITERAL = _LIT_BODY + r'+'

# A key is a literal that a ':' separator follows.
_IS_KEY = r'(?=:(?:\s|$))'

# A number is a number only when the literal run ends where the number does:
# "100m", "1.2.3" and "30s" are digit-leading strings, per "Numbers" in
# docs/tony.md, and a rule that stopped at the number-shaped prefix would paint
# the tail of each of them as something else.
_NUM_END = r'(?!%s|%s)' % (_LIT_CH, _LIT_COLON)

# A tag is '!' followed by non-whitespace, with '.' composing tags and '(...)'
# carrying arguments that are themselves tags: "!retag(tag1.tag2(a,b),tag2(z))".
# Arguments hold commas, so they are matched as a unit rather than left to end
# the tag.
_TAG = r'!(?:[^\s,#\'"\[\]{}()]|\((?:[^()]|\([^()]*\))*\))+'

# A block literal runs to the end of its line and takes every following line
# indented past it, plus any blank lines in between.  \1 is the indentation of
# the opening line, which is what "indented past it" is measured against, so
# a block literal nested in a mapping does not swallow its siblings.
_BLOCK_LITERAL = (
    r'^([ \t]*)'                        # indentation
    r'((?:[^\n]*?[ \t])?)'              # what opens it: a key, '<<:', '- '
    r'(\|[-+]?)'                        # the marker, with its chomping mode
    r'([ \t]*\n)'                       # nothing else on the line
    r'((?:(?:\1[ \t]+[^\n]*)?\n)*)'     # the body
)


class TonyLexer(RegexLexer):
    """
    Lexer for Tony (a YAML dialect).

    Tony-specific features:
    - Tags: !tag, !tag.subtag, !tag(args)
    - Merge keys: <<:
    - Literals holding punctuation: a:b, .[x], $y, no-delegate
    - Digit-leading strings: 100m, 1.2.3
    """
    name = 'Tony'
    aliases = ['tony']
    filenames = ['*.tony']
    mimetypes = ['text/x-tony']

    tokens = {
        'root': [
            # Document separator
            (r'^---[ \t]*$', Punctuation),

            # Comments.  Unlike YAML a comment needs no preceding space, which
            # is also why '#' ends a literal.
            (r'#.*?(TODO|FIXME|XXX|NOTE).*', Comment.Special),
            (r'#.*', Comment.Single),

            # Block literals: |, |-, |+ and the indented body they take
            (_BLOCK_LITERAL,
             bygroups(Text, using(this), Punctuation, Text, String.Heredoc)),

            # Merge keys
            (r'<<' + _IS_KEY, Name.Builtin),

            # Tags
            (_TAG, Keyword),

            # Sparse-array keys, which are base-10 integers
            (r'-?\d+' + _IS_KEY, Name.Label),

            # Quoted strings.  The closing quote is optional so that an
            # unterminated one ends at the line rather than running on.
            (r'"(?:\\.|[^"\\\n])*"?', String.Double),
            (r"'(?:\\.|[^'\\\n])*'?", String.Single),

            # Numbers
            (r'-?0[xX][0-9a-fA-F]+' + _NUM_END, Number.Hex),
            (r'-?0[oO][0-7]+' + _NUM_END, Number.Oct),
            (r'-?0[bB][01]+' + _NUM_END, Number.Bin),
            (r'-?\d+(?:\.\d+)?[eE][-+]?\d+' + _NUM_END, Number.Float),
            (r'-?\d+\.\d+' + _NUM_END, Number.Float),
            (r'-?\d+' + _NUM_END, Number.Integer),

            # Keys, before the constants so that "null: x" reads as a key
            (_LITERAL + _IS_KEY, Name.Attribute),

            # Booleans and null
            (r'null' + _NUM_END, Keyword.Constant),
            (r'(?:true|false)' + _NUM_END, Literal),

            # Separators
            (r':(?=\s|$)', Operator),
            (r'-(?=\s|$)', Punctuation),
            (r'[\[\]{},]', Punctuation),

            # Literals
            (_LITERAL, String.Other),

            # Newline and indentation are matched apart so that a rule
            # anchored at the start of a line is tried there: consuming both
            # at once would carry the position past the anchor.
            (r'\n', Text),
            (r'[ \t]+', Text),

            # Nothing should reach here.  Anything that does is a character
            # this lexer has no rule for, and a highlighter has no business
            # painting a document red over it -- test_lexer.py is where that
            # is caught instead.
            (r'.', Text),
        ],
    }
