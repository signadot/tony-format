// Package token provides tokenization support for Tony and related formats.
//
// [Tokenize] is a function for tokenizing bytes.
//
// [Balance] provides tree structure discovery based on indentation and normalizes
// the token sequence so that it is context free.
//
// [TokenSource] tokenizes a stream from an io.Reader, handing tokens back as
// they complete, and tracks the kinded path of bracketed structure as it goes.
// [TokenSink] writes tokens to an io.Writer, reporting the offset and path at
// which each node starts. [Tokenizer] is the scanner Tokenize and TokenSource
// both drive.
//
// Input is read as Tony unless [TokenYAML] or [TokenJSON] selects another
// format.
//
// The package also holds the quoting rules the encoder and paths share:
// [Quote], [Unquote], [NeedsQuote] and [KPathQuoteField].
package token
