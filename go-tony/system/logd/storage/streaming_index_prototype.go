package storage

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/token"
)

// StreamingIndexer reads a document from disk using streaming tokenization,
// populates the index.Index structure, and writes tokens to another file.
// It never stores the entire document in memory, only maintaining the index.
type StreamingIndexer struct {
	// Input: source file to read from
	sourceFile *os.File

	// Output: destination file to write to
	destFile *os.File

	// Token source for reading
	source *token.TokenSource

	// Token sink for writing
	sink *token.TokenSink

	// Index being built (maintained in memory)
	idx *index.Index

	// Current commit/tx for index entries
	commit int64
	tx     int64

	// Track paths we've seen (to avoid duplicate entries)
	// Maps path -> true if we've already indexed it
	seenPaths map[string]bool
}

// NewStreamingIndexer creates a new streaming indexer.
// It opens the source file for reading and creates/opens the dest file for writing.
func NewStreamingIndexer(sourcePath, destPath string, commit, tx int64) (*StreamingIndexer, error) {
	// Open source file for reading
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open source file: %w", err)
	}

	// Create/open dest file for writing
	destFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		sourceFile.Close()
		return nil, fmt.Errorf("failed to open dest file: %w", err)
	}

	// Create token source from source file
	source := token.NewTokenSource(sourceFile)

	// Create indexer first (we need it for the callback)
	si := &StreamingIndexer{
		sourceFile: sourceFile,
		destFile:   destFile,
		source:     source,
		idx:        index.NewIndex(""),
		commit:     commit,
		tx:         tx,
		seenPaths:  make(map[string]bool),
	}

	// Create token sink with callback that updates the index
	// The callback receives the absolute byte offset in the output file and kpath
	sink := token.NewTokenSink(destFile, func(offset int, kpath string, tok token.Token) {
		// TokenSink already provides kpath format (e.g., "a.b.c", "[0].key", "foo{4}.bar")

		// Check if we've already indexed this path
		if si.seenPaths[kpath] {
			return // Already indexed
		}

		// Add to index
		seg := &index.LogSegment{
			StartCommit: si.commit,
			EndCommit:   si.commit,
			StartTx:     si.tx,
			EndTx:       si.tx,
			KindedPath:  kpath,         // Full kinded path from root (e.g., "a.b.c", "[0].key", "foo{4}.bar")
			LogPosition: int64(offset), // Use the offset from TokenSink (absolute byte position)
		}

		si.idx.Add(seg)
		si.seenPaths[kpath] = true
	})

	si.sink = sink

	return si, nil
}

// Process streams tokens from source to destination, building the index as it goes.
// It never loads the entire document into memory - only processes tokens incrementally.
// The TokenSink callback automatically updates the index when nodes start.
func (si *StreamingIndexer) Process() error {
	// Process tokens in a loop
	for {
		// Read tokens from source
		tokens, err := si.source.Read()
		if err == io.EOF {
			// EOF reached - flush any remaining tokens and finish
			if len(tokens) > 0 {
				if err := si.sink.Write(tokens); err != nil {
					return fmt.Errorf("failed to write final tokens: %w", err)
				}
			}
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tokens: %w", err)
		}

		// Write tokens to sink
		// This will trigger the callback (set in NewStreamingIndexer) which updates the index
		// The callback receives the absolute byte offset in the output file
		if err := si.sink.Write(tokens); err != nil {
			return fmt.Errorf("failed to write tokens: %w", err)
		}
	}

	return nil
}

// GetIndex returns the built index.
func (si *StreamingIndexer) GetIndex() *index.Index {
	return si.idx
}

// ReadPath reads a specific path from the destination file using the index.
// It looks up the path in the index, reads from the LogPosition offset,
// parses the node, and extracts the specific path.
//
// The implementation reads directly from the file starting at the offset,
// allowing parsing to read as much as necessary to find a complete node,
// even if it exceeds any initial size estimate. This ensures we can parse
// nodes of any size without artificial limits.
//
// Both bracketed structures ({...} or [...]) and simple values (strings, numbers,
// booleans, null) are supported. The implementation detects the token type
// and uses the appropriate parsing method:
// - Simple values: parsed directly from tokens
// - Bracketed structures: parsed using ParseNodeFromSource
//
// The kpath parameter should be in kpath format (e.g., "a.b.c", "[0].key", "foo{4}.bar")
func (si *StreamingIndexer) ReadPath(kpath string) (*ir.Node, error) {
	// Look up path in index
	from := si.commit
	to := si.commit
	segments := si.idx.LookupRange(kpath, &from, &to)

	if len(segments) == 0 {
		return nil, fmt.Errorf("path not found in index: %s", kpath)
	}

	// Use the first segment (could have multiple if path appears multiple times)
	seg := segments[0]

	// Get the destination file path - we need to reopen it for reading
	// (since we might have written to it and the file handle might be at EOF)
	destPath := si.destFile.Name()

	// Reopen file for reading
	readFile, err := os.Open(destPath)
	if err != nil {
		return nil, fmt.Errorf("failed to reopen dest file for reading: %w", err)
	}
	defer readFile.Close()

	// Seek to the offset where the node starts
	_, err = readFile.Seek(seg.LogPosition, io.SeekStart)
	if err != nil {
		return nil, fmt.Errorf("failed to seek to offset %d: %w", seg.LogPosition, err)
	}

	// Create token source directly from the file (starting at current position)
	// This allows us to read as much as needed until we find a complete node.
	// We don't use SectionReader here because we want to read beyond any initial
	// size estimate if necessary. The file will naturally stop at EOF.
	source := token.NewTokenSource(readFile)

	// Read tokens until we find a non-whitespace/indent token
	// The offset might point to whitespace before the actual value
	var firstToken token.Token
	found := false
	for !found {
		tokens, err := source.Read()
		if err != nil {
			return nil, fmt.Errorf("failed to read tokens at offset %d: %w", seg.LogPosition, err)
		}
		if len(tokens) == 0 {
			return nil, fmt.Errorf("no tokens found at offset %d", seg.LogPosition)
		}

		// Skip whitespace, indent, and comment tokens
		for _, tok := range tokens {
			if tok.Type != token.TIndent && tok.Type != token.TComment {
				// Check if it's whitespace (spaces/tabs) - these might not have a specific token type
				// but if they do, we'd skip them here. For now, accept any non-indent/comment token.
				firstToken = tok
				found = true
				break
			}
		}
		if !found {
			// All tokens were whitespace/indent - continue reading
			continue
		}
	}

	// Check if this is a simple value (non-bracketed)
	var node *ir.Node
	if isSimpleValueToken(firstToken.Type) {
		// Parse simple value directly from token
		node, err = parseSimpleValue(firstToken, source)
		if err != nil {
			return nil, fmt.Errorf("failed to parse simple value at offset %d: %w", seg.LogPosition, err)
		}
	} else if firstToken.Type == token.TLCurl || firstToken.Type == token.TLSquare {
		// It's a bracketed structure - use parseNodeFromSource
		// We've already skipped whitespace and found the opening bracket
		// We need to reseek to the start of the bracket (before the firstToken)
		// Get current file position and calculate where the bracket starts
		currentPos, err := readFile.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, fmt.Errorf("failed to get current position: %w", err)
		}
		// The firstToken we found is the opening bracket - reseek to just before it
		bracketStart := currentPos - int64(len(firstToken.Bytes))
		_, err = readFile.Seek(bracketStart, io.SeekStart)
		if err != nil {
			return nil, fmt.Errorf("failed to reseek to bracket start: %w", err)
		}
		source = token.NewTokenSource(readFile)
		node, err = parse.ParseNodeFromSource(source)
		if err != nil {
			return nil, fmt.Errorf("failed to parse bracketed node at offset %d: %w", seg.LogPosition, err)
		}
	} else {
		return nil, fmt.Errorf("unexpected token type %s at offset %d (expected bracketed structure or simple value)", firstToken.Type, seg.LogPosition)
	}

	// Check if the node we read is already at the exact path we want
	// (i.e., the index entry points directly to this path)
	if seg.KindedPath == kpath {
		// The node we read IS the path we want - return it directly
		// For simple values, this is always the case.
		// For bracketed structures, if the path matches exactly, the node is what we want.
		return node, nil
	}

	// The node we read contains the path we want as a sub-path
	// Extract the specific path from the node using kpath directly
	result, err := node.GetKPath(kpath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract path %s from node: %w", kpath, err)
	}

	if result == nil {
		return nil, fmt.Errorf("path %s not found in node", kpath)
	}

	return result, nil
}

// ReadRandomPaths reads multiple random paths from the index and extracts their nodes.
// It demonstrates using the index to find and read paths.
func (si *StreamingIndexer) ReadRandomPaths(paths []string) (map[string]*ir.Node, error) {
	results := make(map[string]*ir.Node)

	for _, path := range paths {
		node, err := si.ReadPath(path)
		if err != nil {
			// Log error but continue with other paths
			fmt.Printf("Warning: failed to read path %s: %v\n", path, err)
			continue
		}
		results[path] = node
	}

	return results, nil
}

// isNumeric checks if a string represents a number.
func isNumeric(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isSimpleValueToken checks if a token type represents a simple (non-bracketed) value.
func isSimpleValueToken(tokType token.TokenType) bool {
	switch tokType {
	case token.TString, token.TMString, token.TLiteral, token.TMLit,
		token.TInteger, token.TFloat, token.TTrue, token.TFalse, token.TNull:
		return true
	default:
		return false
	}
}

// parseSimpleValue parses a simple value token into an ir.Node.
// It handles the token and any trailing comments.
func parseSimpleValue(tok token.Token, source *token.TokenSource) (*ir.Node, error) {
	var node *ir.Node

	switch tok.Type {
	case token.TString, token.TLiteral:
		node = ir.FromString(tok.String())
	case token.TMString, token.TMLit:
		node = ir.FromString(tok.String())
		// TMString may have multiple lines
		if tok.Type == token.TMString {
			parts := strings.Split(string(tok.Bytes), "\n")
			for _, part := range parts {
				node.Lines = append(node.Lines, token.QuotedToString([]byte(part)))
			}
		}
	case token.TInteger:
		i, err := strconv.ParseInt(string(tok.Bytes), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer: %w", err)
		}
		node = ir.FromInt(i)
	case token.TFloat:
		f, err := strconv.ParseFloat(string(tok.Bytes), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float: %w", err)
		}
		node = ir.FromFloat(f)
	case token.TTrue:
		node = ir.FromBool(true)
	case token.TFalse:
		node = ir.FromBool(false)
	case token.TNull:
		node = ir.Null()
	default:
		return nil, fmt.Errorf("unexpected token type for simple value: %s", tok.Type)
	}

	// Read any trailing comments (similar to checkLineComment in parse package)
	// Comments are optional, so we continue reading until we hit a non-comment token
	for {
		moreTokens, err := source.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading trailing tokens: %w", err)
		}
		if len(moreTokens) == 0 {
			break
		}

		// Check if next token is a comment
		nextToken := moreTokens[0]
		if nextToken.Type == token.TComment {
			// Skip comments for now (we could attach them to node.Comment if needed)
			// For simplicity, we'll just skip them
			continue
		}
		// Not a comment - we're done with this value
		break
	}

	return node, nil
}

// Close closes the source and destination files.
func (si *StreamingIndexer) Close() error {
	var errs []error

	if err := si.sourceFile.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close source file: %w", err))
	}

	if err := si.destFile.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close dest file: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing files: %v", errs)
	}

	return nil
}

// Example usage function
func ExampleStreamingIndexer() error {
	// Example: process a document file
	sourcePath := "input.tony"
	destPath := "output.tony"
	commit := int64(1)
	tx := int64(1)

	// Create indexer
	indexer, err := NewStreamingIndexer(sourcePath, destPath, commit, tx)
	if err != nil {
		return fmt.Errorf("failed to create indexer: %w", err)
	}
	defer indexer.Close()

	// Process the document
	if err := indexer.Process(); err != nil {
		return fmt.Errorf("failed to process document: %w", err)
	}

	// Get the built index
	idx := indexer.GetIndex()

	// Use the index for lookups
	// Example: lookup all paths in commit range
	from := int64(1)
	to := int64(1)
	segments := idx.LookupRange("", &from, &to)

	fmt.Printf("Indexed %d paths\n", len(segments))
	for _, seg := range segments {
		fmt.Printf("  Path: %s, Offset: %d\n", seg.KindedPath, seg.LogPosition)
	}

	// Example: Read random paths using the index
	if len(segments) > 0 {
		// Pick a few random paths to read (paths are in kpath format)
		pathsToRead := []string{}
		for i := 0; i < len(segments) && i < 5; i++ {
			if segments[i].KindedPath != "" {
				pathsToRead = append(pathsToRead, segments[i].KindedPath)
			}
		}

		fmt.Printf("\nReading %d paths from file:\n", len(pathsToRead))
		nodes, err := indexer.ReadRandomPaths(pathsToRead)
		if err != nil {
			return fmt.Errorf("failed to read paths: %w", err)
		}

		for path, node := range nodes {
			fmt.Printf("  Path: %s\n", path)
			fmt.Printf("    Node type: %v\n", node.Type)
			if node.Type == ir.StringType {
				fmt.Printf("    Value: %s\n", node.String)
			} else if node.Type == ir.NumberType {
				if node.Int64 != nil {
					fmt.Printf("    Value: %d\n", *node.Int64)
				} else if node.Float64 != nil {
					fmt.Printf("    Value: %f\n", *node.Float64)
				}
			} else if node.Type == ir.BoolType {
				fmt.Printf("    Value: %v\n", node.Bool)
			}
		}
	}

	return nil
}
