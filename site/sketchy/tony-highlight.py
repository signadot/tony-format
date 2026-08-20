#!/usr/bin/env python3
"""
Quick script to highlight Tony code in terminal.

Usage:
    python3 tony-highlight.py < file.tony
    python3 tony-highlight.py file.tony
    echo '!tag-name\n- item' | python3 tony-highlight.py
"""

import sys
from pygments import highlight
from pygments.lexers import get_lexer_by_name
from pygments.formatters import Terminal256Formatter

def main():
    # Read from stdin or file
    if len(sys.argv) > 1:
        with open(sys.argv[1], 'r') as f:
            code = f.read()
    else:
        code = sys.stdin.read()
    
    try:
        lexer = get_lexer_by_name('tony')
        formatter = Terminal256Formatter(style='default')
        print(highlight(code, lexer, formatter), end='')
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)

if __name__ == '__main__':
    main()
