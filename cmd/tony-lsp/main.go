package main

import (
	"context"
	"io"
	"os"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

const lsName = "tony-lsp"

var (
	version = "0.0.1"
)

func main() {
	ctx := context.Background()
	stream := jsonrpc2.NewStream(&stdioReadWriteCloser{
		read:  os.Stdin,
		write: os.Stdout,
	})
	server := &Server{}
	server.setupHandlers(ctx)
	handler := protocol.ServerHandler(server, nil)
	conn := jsonrpc2.NewConn(stream)
	server.conn = conn
	conn.Go(ctx, handler)
	<-conn.Done()
}

type stdioReadWriteCloser struct {
	read  io.Reader
	write io.Writer
}

func (s *stdioReadWriteCloser) Read(p []byte) (n int, err error) {
	return s.read.Read(p)
}

func (s *stdioReadWriteCloser) Write(p []byte) (n int, err error) {
	return s.write.Write(p)
}

func (s *stdioReadWriteCloser) Close() error {
	return nil
}

type Server struct {
	conn jsonrpc2.Conn
	docs *documentStore
}

func (s *Server) setupHandlers(ctx context.Context) {
	s.docs = &documentStore{
		docs: make(map[string]*document),
	}
}

func (s *Server) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	capabilities := protocol.ServerCapabilities{
		TextDocumentSync: &protocol.TextDocumentSyncOptions{
			Change:    protocol.TextDocumentSyncKindIncremental,
			OpenClose: true,
			Save:      &protocol.SaveOptions{IncludeText: false},
		},
		HoverProvider: true,
		DocumentFormattingProvider: true,
		CompletionProvider: &protocol.CompletionOptions{
			TriggerCharacters: []string{":", "!", "-", "[", "{"},
		},
		SemanticTokensProvider: map[string]interface{}{
			"full": true,
			"range": true,
			"legend": protocol.SemanticTokensLegend{
				TokenTypes: []protocol.SemanticTokenTypes{
					protocol.SemanticTokenComment,
					protocol.SemanticTokenKeyword,
					protocol.SemanticTokenString,
					protocol.SemanticTokenNumber,
					protocol.SemanticTokenOperator,
					protocol.SemanticTokenProperty,
				},
				TokenModifiers: []protocol.SemanticTokenModifiers{
					protocol.SemanticTokenModifierDefinition,
					protocol.SemanticTokenModifierModification,
				},
			},
		},
	}

	return &protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &protocol.ServerInfo{
			Name:    lsName,
			Version: version,
		},
	}, nil
}

func (s *Server) Initialized(ctx context.Context, params *protocol.InitializedParams) error {
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return nil
}

func (s *Server) Exit(ctx context.Context) error {
	return nil
}

func (s *Server) SetTrace(ctx context.Context, params *protocol.SetTraceParams) error {
	return nil
}

// Implement other required Server interface methods with empty implementations
func (s *Server) WorkDoneProgressCancel(ctx context.Context, params *protocol.WorkDoneProgressCancelParams) error { return nil }
func (s *Server) LogTrace(ctx context.Context, params *protocol.LogTraceParams) error { return nil }
func (s *Server) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error) { return nil, nil }
func (s *Server) CodeLens(ctx context.Context, params *protocol.CodeLensParams) ([]protocol.CodeLens, error) { return nil, nil }
func (s *Server) CodeLensResolve(ctx context.Context, params *protocol.CodeLens) (*protocol.CodeLens, error) { return nil, nil }
func (s *Server) ColorPresentation(ctx context.Context, params *protocol.ColorPresentationParams) ([]protocol.ColorPresentation, error) { return nil, nil }
func (s *Server) CompletionResolve(ctx context.Context, params *protocol.CompletionItem) (*protocol.CompletionItem, error) { return nil, nil }
func (s *Server) Declaration(ctx context.Context, params *protocol.DeclarationParams) ([]protocol.Location, error) { return nil, nil }
func (s *Server) Definition(ctx context.Context, params *protocol.DefinitionParams) ([]protocol.Location, error) { return nil, nil }
func (s *Server) DidChangeConfiguration(ctx context.Context, params *protocol.DidChangeConfigurationParams) error { return nil }
func (s *Server) DidChangeWatchedFiles(ctx context.Context, params *protocol.DidChangeWatchedFilesParams) error { return nil }
func (s *Server) DidChangeWorkspaceFolders(ctx context.Context, params *protocol.DidChangeWorkspaceFoldersParams) error { return nil }
func (s *Server) DidSave(ctx context.Context, params *protocol.DidSaveTextDocumentParams) error { return nil }
func (s *Server) DocumentColor(ctx context.Context, params *protocol.DocumentColorParams) ([]protocol.ColorInformation, error) { return nil, nil }
func (s *Server) DocumentHighlight(ctx context.Context, params *protocol.DocumentHighlightParams) ([]protocol.DocumentHighlight, error) { return nil, nil }
func (s *Server) DocumentLink(ctx context.Context, params *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) { return nil, nil }
func (s *Server) DocumentLinkResolve(ctx context.Context, params *protocol.DocumentLink) (*protocol.DocumentLink, error) { return nil, nil }
func (s *Server) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) ([]interface{}, error) { return nil, nil }
func (s *Server) ExecuteCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (interface{}, error) { return nil, nil }
func (s *Server) FoldingRanges(ctx context.Context, params *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) { return nil, nil }
func (s *Server) Implementation(ctx context.Context, params *protocol.ImplementationParams) ([]protocol.Location, error) { return nil, nil }
func (s *Server) OnTypeFormatting(ctx context.Context, params *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) { return nil, nil }
func (s *Server) PrepareRename(ctx context.Context, params *protocol.PrepareRenameParams) (*protocol.Range, error) { return nil, nil }
func (s *Server) RangeFormatting(ctx context.Context, params *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) { return nil, nil }
func (s *Server) References(ctx context.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) { return nil, nil }
func (s *Server) Rename(ctx context.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) { return nil, nil }
func (s *Server) SignatureHelp(ctx context.Context, params *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) { return nil, nil }
func (s *Server) Symbols(ctx context.Context, params *protocol.WorkspaceSymbolParams) ([]protocol.SymbolInformation, error) { return nil, nil }
func (s *Server) TypeDefinition(ctx context.Context, params *protocol.TypeDefinitionParams) ([]protocol.Location, error) { return nil, nil }
func (s *Server) WillSave(ctx context.Context, params *protocol.WillSaveTextDocumentParams) error { return nil }
func (s *Server) WillSaveWaitUntil(ctx context.Context, params *protocol.WillSaveTextDocumentParams) ([]protocol.TextEdit, error) { return nil, nil }
func (s *Server) ShowDocument(ctx context.Context, params *protocol.ShowDocumentParams) (*protocol.ShowDocumentResult, error) { return nil, nil }
func (s *Server) WillCreateFiles(ctx context.Context, params *protocol.CreateFilesParams) (*protocol.WorkspaceEdit, error) { return nil, nil }
func (s *Server) DidCreateFiles(ctx context.Context, params *protocol.CreateFilesParams) error { return nil }
func (s *Server) WillRenameFiles(ctx context.Context, params *protocol.RenameFilesParams) (*protocol.WorkspaceEdit, error) { return nil, nil }
func (s *Server) DidRenameFiles(ctx context.Context, params *protocol.RenameFilesParams) error { return nil }
func (s *Server) WillDeleteFiles(ctx context.Context, params *protocol.DeleteFilesParams) (*protocol.WorkspaceEdit, error) { return nil, nil }
func (s *Server) DidDeleteFiles(ctx context.Context, params *protocol.DeleteFilesParams) error { return nil }
func (s *Server) CodeLensRefresh(ctx context.Context) error { return nil }
func (s *Server) PrepareCallHierarchy(ctx context.Context, params *protocol.CallHierarchyPrepareParams) ([]protocol.CallHierarchyItem, error) { return nil, nil }
func (s *Server) IncomingCalls(ctx context.Context, params *protocol.CallHierarchyIncomingCallsParams) ([]protocol.CallHierarchyIncomingCall, error) { return nil, nil }
func (s *Server) OutgoingCalls(ctx context.Context, params *protocol.CallHierarchyOutgoingCallsParams) ([]protocol.CallHierarchyOutgoingCall, error) { return nil, nil }
// SemanticTokensFull, SemanticTokensRange are implemented in semantic_tokens.go
func (s *Server) SemanticTokensFullDelta(ctx context.Context, params *protocol.SemanticTokensDeltaParams) (interface{}, error) { return nil, nil }
func (s *Server) SemanticTokensRefresh(ctx context.Context) error { return nil }
func (s *Server) LinkedEditingRange(ctx context.Context, params *protocol.LinkedEditingRangeParams) (*protocol.LinkedEditingRanges, error) { return nil, nil }
func (s *Server) Moniker(ctx context.Context, params *protocol.MonikerParams) ([]protocol.Moniker, error) { return nil, nil }
func (s *Server) Request(ctx context.Context, method string, params interface{}) (interface{}, error) { return nil, nil }
