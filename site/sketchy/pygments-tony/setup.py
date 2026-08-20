"""
Setup script for Pygments Tony lexer.

Install with:
    pip install -e .
    
Or for development:
    pip install -e .[dev]
"""

from setuptools import setup

setup(
    name='pygments-tony',
    version='0.1.0',
    description='Pygments lexer for Tony syntax highlighting',
    author='Tony Project',
    py_modules=['pygments_tony'],
    install_requires=[
        'pygments>=2.0',
    ],
    python_requires='>=3.6',
    extras_require={
        'dev': ['pytest'],
    },
    entry_points={
        'pygments.lexers': [
            'tony = pygments_tony:TonyLexer',
        ],
    },
    classifiers=[
        'Development Status :: 3 - Alpha',
        'Intended Audience :: Developers',
        'License :: OSI Approved :: MIT License',
        'Programming Language :: Python :: 3',
        'Topic :: Software Development :: Libraries :: Python Modules',
    ],
)
