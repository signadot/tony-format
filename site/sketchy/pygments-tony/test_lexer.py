#!/usr/bin/env python3
"""
Tests for the Tony lexer.

Run with:
    python test_lexer.py     # no pytest needed
    pytest test_lexer.py

The sweep at the end lexes every ```tony block in the docs and fails on any
character the lexer has no rule for.  The lexer paints those Text rather than
Error -- a highlighter that decides a document is wrong and turns it red is
worse than one that leaves it plain -- so this is where such a character has
to be caught.
"""

import pathlib
import re
import sys

from pygments.token import (
    Comment, Error, Keyword, Literal, Name, Number, Operator, Punctuation,
    String, Text
)

sys.path.insert(0, str(pathlib.Path(__file__).parent))
from pygments_tony import TonyLexer  # noqa: E402

LEXER = TonyLexer()
DOCS = pathlib.Path(__file__).resolve().parents[2]
FENCE = re.compile(r'^([ \t]*)```+tony[^\n]*\n(.*?)^\1```+', re.M | re.S)


def toks(code):
    """The lexer's output, with whitespace-only tokens dropped."""
    return [(t, v) for t, v in LEXER.get_tokens(code) if v.strip()]


def check(code, *expected):
    got = toks(code)
    assert got == list(expected), (
        'lexing %r\n  got      %r\n  expected %r' % (code, got, list(expected)))


def test_keys_and_values():
    check('name: blog-api\n',
          (Name.Attribute, 'name'), (Operator, ':'), (String.Other, 'blog-api'))
    check('count: 3\n',
          (Name.Attribute, 'count'), (Operator, ':'), (Number.Integer, '3'))
    check('k: null\n',
          (Name.Attribute, 'k'), (Operator, ':'), (Keyword.Constant, 'null'))
    check('k: true\n',
          (Name.Attribute, 'k'), (Operator, ':'), (Literal, 'true'))
    # a key named for a constant is still a key
    check('null: 1\n',
          (Name.Attribute, 'null'), (Operator, ':'), (Number.Integer, '1'))
    # and a literal that merely starts with one is neither
    check('nullable\n', (String.Other, 'nullable'))


def test_literal_punctuation():
    """docs/tony.md, "Literals and Punctuation"."""
    # ':' inside a literal, on both sides of the separator
    check('a:b: "a:b"\n',
          (Name.Attribute, 'a:b'), (Operator, ':'), (String.Double, '"a:b"'))
    check('resolver: resolvers:security:security-review\n',
          (Name.Attribute, 'resolver'), (Operator, ':'),
          (String.Other, 'resolvers:security:security-review'))
    # '-' and '.' and '/' inside a literal
    check('- tony-format/context\n',
          (Punctuation, '-'), (String.Other, 'tony-format/context'))
    check('document: my-file.yaml\n',
          (Name.Attribute, 'document'), (Operator, ':'),
          (String.Other, 'my-file.yaml'))
    # leading '$' and '.', and balanced brackets
    check('$y: "$y"\n',
          (Name.Attribute, '$y'), (Operator, ':'), (String.Double, '"$y"'))
    check('.[x]: ".[x]"\n',
          (Name.Attribute, '.[x]'), (Operator, ':'), (String.Double, '".[x]"'))
    check('fields: .array(.field)\n',
          (Name.Attribute, 'fields'), (Operator, ':'),
          (String.Other, '.array(.field)'))
    # an un-opened close character truncates the literal
    check('{a:b}\n',
          (Punctuation, '{'), (String.Other, 'a:b'), (Punctuation, '}'))


def test_numbers():
    """docs/tony.md, "Numbers" -- rules 1, 3 and 4."""
    check('{ replicas: 3, ratio: 1.5, big: 1e9, neg: -2.5 }\n',
          (Punctuation, '{'),
          (Name.Attribute, 'replicas'), (Operator, ':'), (Number.Integer, '3'),
          (Punctuation, ','),
          (Name.Attribute, 'ratio'), (Operator, ':'), (Number.Float, '1.5'),
          (Punctuation, ','),
          (Name.Attribute, 'big'), (Operator, ':'), (Number.Float, '1e9'),
          (Punctuation, ','),
          (Name.Attribute, 'neg'), (Operator, ':'), (Number.Float, '-2.5'),
          (Punctuation, '}'))
    # rule 3: a run holding a letter is a string, all of it
    for quantity in ('100m', '1Gi', '30s', '1h30m'):
        check('k: %s\n' % quantity,
              (Name.Attribute, 'k'), (Operator, ':'), (String.Other, quantity))
    # rule 4: three or more '.'-separated groups of digits is a string
    check('addr: 192.168.1.1\n',
          (Name.Attribute, 'addr'), (Operator, ':'),
          (String.Other, '192.168.1.1'))
    # notation is part of the number
    check('{ h: 0x1f, o: 0o644, b: 0b1010 }\n',
          (Punctuation, '{'),
          (Name.Attribute, 'h'), (Operator, ':'), (Number.Hex, '0x1f'),
          (Punctuation, ','),
          (Name.Attribute, 'o'), (Operator, ':'), (Number.Oct, '0o644'),
          (Punctuation, ','),
          (Name.Attribute, 'b'), (Operator, ':'), (Number.Bin, '0b1010'),
          (Punctuation, '}'))
    # a sparse-array key is a key, not a value
    check('0: hello\n',
          (Name.Label, '0'), (Operator, ':'), (String.Other, 'hello'))


def test_tags():
    check('!my-tag 2\n', (Keyword, '!my-tag'), (Number.Integer, '2'))
    check('document: !tovalue.file my-file.yaml\n',
          (Name.Attribute, 'document'), (Operator, ':'),
          (Keyword, '!tovalue.file'), (String.Other, 'my-file.yaml'))
    # arguments, including the commas within them, belong to the tag
    check('f: !tag1.tag2(a,b) 22\n',
          (Name.Attribute, 'f'), (Operator, ':'),
          (Keyword, '!tag1.tag2(a,b)'), (Number.Integer, '22'))
    check('f: !retag(tag1.tag2(a,b),tag2(z).other(x))\n',
          (Name.Attribute, 'f'), (Operator, ':'),
          (Keyword, '!retag(tag1.tag2(a,b),tag2(z).other(x))'))
    # a tag in a key set ends at the ',' that separates elements
    check('[!a, !b]\n',
          (Punctuation, '['), (Keyword, '!a'), (Punctuation, ','),
          (Keyword, '!b'), (Punctuation, ']'))


def test_comments_and_separators():
    check('---\n', (Punctuation, '---'))
    check('# a comment\n', (Comment.Single, '# a comment'))
    # no preceding space required
    check('k: v# tail\n',
          (Name.Attribute, 'k'), (Operator, ':'), (String.Other, 'v'),
          (Comment.Single, '# tail'))
    check('[1, 2, 3,]\n',
          (Punctuation, '['), (Number.Integer, '1'), (Punctuation, ','),
          (Number.Integer, '2'), (Punctuation, ','), (Number.Integer, '3'),
          (Punctuation, ','), (Punctuation, ']'))
    check('[1 2 3]\n',
          (Punctuation, '['), (Number.Integer, '1'), (Number.Integer, '2'),
          (Number.Integer, '3'), (Punctuation, ']'))


def test_block_literals():
    check('|\n  hello\n  I am a block literal\n',
          (Punctuation, '|'),
          (String.Heredoc, '  hello\n  I am a block literal\n'))
    check('|-\n  chopped\n',
          (Punctuation, '|-'), (String.Heredoc, '  chopped\n'))
    # opened by a key, and by a merge key
    check('k: |\n  hello\n',
          (Name.Attribute, 'k'), (Operator, ':'), (Punctuation, '|'),
          (String.Heredoc, '  hello\n'))
    check('<<: |\n  {{ $k | quote}}: {{ $v | quote -}}\n',
          (Name.Builtin, '<<'), (Operator, ':'), (Punctuation, '|'),
          (String.Heredoc, '  {{ $k | quote}}: {{ $v | quote -}}\n'))
    # the body is what is indented past the opening line: a sibling of the key
    # that opened it is not part of it
    check('a:\n  k: |\n    hello\n  j: 2\n',
          (Name.Attribute, 'a'), (Operator, ':'),
          (Name.Attribute, 'k'), (Operator, ':'), (Punctuation, '|'),
          (String.Heredoc, '    hello\n'),
          (Name.Attribute, 'j'), (Operator, ':'), (Number.Integer, '2'))
    # '|' is only a marker at the end of a line
    check('k: a|b\n',
          (Name.Attribute, 'k'), (Operator, ':'), (String.Other, 'a|b'))


def test_strings():
    check("b: 'single \"quoted\"'\n",
          (Name.Attribute, 'b'), (Operator, ':'),
          (String.Single, "'single \"quoted\"'"))
    # folded strings are one per line
    check('c:\n  "part one"\n  " and two"\n',
          (Name.Attribute, 'c'), (Operator, ':'),
          (String.Double, '"part one"'), (String.Double, '" and two"'))
    # an escaped quote does not end the string
    check(r'k: "a \" b"' + '\n',
          (Name.Attribute, 'k'), (Operator, ':'),
          (String.Double, r'"a \" b"'))
    # an unterminated string ends at the line, taking nothing after it
    check('k: "oops\nj: 1\n',
          (Name.Attribute, 'k'), (Operator, ':'), (String.Double, '"oops'),
          (Name.Attribute, 'j'), (Operator, ':'), (Number.Integer, '1'))


def test_docs_have_no_unlexed_characters():
    """Every ```tony block in the docs, lexed."""
    blocks = 0
    bad = []
    for md in sorted(DOCS.rglob('*.md')):
        text = md.read_text()
        for m in FENCE.finditer(text):
            blocks += 1
            line = text[:m.start()].count('\n') + 2
            for tok, val in LEXER.get_tokens(m.group(2)):
                if tok is Error or (tok is Text and val.strip()):
                    bad.append('%s:%d: %s %r' % (md.name, line, tok, val))
    assert blocks > 100, 'expected the docs to hold tony blocks, found %d' % blocks
    assert not bad, 'unlexed characters in %d blocks:\n  %s' % (
        blocks, '\n  '.join(bad[:40]))


if __name__ == '__main__':
    failures = 0
    for name, fn in sorted(globals().items()):
        if not name.startswith('test_'):
            continue
        try:
            fn()
            print('ok   %s' % name)
        except AssertionError as e:
            failures += 1
            print('FAIL %s\n%s' % (name, e))
    sys.exit(1 if failures else 0)
