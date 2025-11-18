#!/usr/bin/env python3
"""
Quick test script to verify the Tony lexer is working.

Run with:
    python test_lexer.py
"""

try:
    from pygments import highlight
    from pygments.formatters import TerminalFormatter, HtmlFormatter
    from pygments import lexers
    
    # Try to get the Tony lexer
    try:
        lexer = lexers.get_lexer_by_name('tony')
        print(f"✓ Found Tony lexer: {lexer.name}")
    except Exception as e:
        print(f"✗ Could not find Tony lexer: {e}")
        print("\nAvailable lexers containing 'tony':")
        all_lexers = lexers.get_all_lexers()
        tony_lexers = [x for x in all_lexers if 'tony' in x[0].lower()]
        if tony_lexers:
            for name, aliases, _, _ in tony_lexers:
                print(f"  - {name} (aliases: {aliases})")
        else:
            print("  None found")
        exit(1)
    
    # Test code
    code = '''!tag
key: value
<<: merge
$[var]
.[path]
# comment
'''
    
    # Test terminal output
    print("\n" + "="*50)
    print("Terminal Output:")
    print("="*50)
    result = highlight(code, lexer, TerminalFormatter())
    print(result)
    
    # Test HTML output
    print("\n" + "="*50)
    print("HTML Output (first 500 chars):")
    print("="*50)
    html_result = highlight(code, lexer, HtmlFormatter())
    print(html_result[:500] + "...")
    
    print("\n✓ Lexer is working correctly!")
    
except ImportError as e:
    print(f"✗ Import error: {e}")
    print("\nMake sure Pygments is installed:")
    print("  pip install pygments")
    exit(1)
except Exception as e:
    print(f"✗ Error: {e}")
    import traceback
    traceback.print_exc()
    exit(1)
