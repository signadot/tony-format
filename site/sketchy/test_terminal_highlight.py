#!/usr/bin/env python3
"""Test Tony syntax highlighting in terminal using Pygments."""

from pygments import highlight
from pygments.lexers import get_lexer_by_name
from pygments.formatters import TerminalFormatter, Terminal256Formatter
import sys

def highlight_tony(code, use_256=True):
    """Highlight Tony code for terminal output."""
    try:
        lexer = get_lexer_by_name('tony')
    except Exception as e:
        print(f"Error: Could not find Tony lexer: {e}", file=sys.stderr)
        print("Make sure pygments-tony is installed:", file=sys.stderr)
        print("  pip install -e docs/pygments-tony/", file=sys.stderr)
        return None
    
    # Use Terminal256Formatter for better colors if available
    if use_256:
        try:
            formatter = Terminal256Formatter(style='default')
        except:
            formatter = TerminalFormatter()
    else:
        formatter = TerminalFormatter()
    
    return highlight(code, lexer, formatter)

if __name__ == '__main__':
    # Test code
    test_code = """# Tony example
f:
- I
- am
- indented
- by
- the
- array
- elements
- token
- "- " 
-
  period
"""
    
    result = highlight_tony(test_code)
    if result:
        print(result)
    else:
        sys.exit(1)
