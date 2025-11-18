package server

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)


// Server represents the logd HTTP server.
type Server struct {
	storage *storage.Storage
	txWaitersMu sync.Mutex
	txWaiters   map[string]*transactionWaiter // transactionID -> waiter
}

// New creates a new Server instance.
func New(s *storage.Storage) *Server {
	return &Server{
		storage: s,
		txWaiters: make(map[string]*transactionWaiter),
	}
}

// getOrCreateWaiter gets or creates a waiter for a transaction.
func (s *Server) getOrCreateWaiter(transactionID string) *transactionWaiter {
	s.txWaitersMu.Lock()
	defer s.txWaitersMu.Unlock()
	
	waiter, exists := s.txWaiters[transactionID]
	if !exists {
		waiter = NewTransactionWaiter()
		s.txWaiters[transactionID] = waiter
	}
	return waiter
}

// removeWaiter removes a waiter for a transaction.
func (s *Server) removeWaiter(transactionID string) {
	s.txWaitersMu.Lock()
	defer s.txWaitersMu.Unlock()
	delete(s.txWaiters, transactionID)
}


// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only handle /api/data for now
	if r.URL.Path != "/api/data" {
		http.NotFound(w, r)
		return
	}

	// Parse request body
	body, err := api.ParseRequestBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, fmt.Sprintf("failed to parse request body: %v", err)))
		return
	}

	// Route based on HTTP method
	switch r.Method {
	case "MATCH":
		s.handleMatch(w, r, body)
	case "PATCH":
		s.handlePatch(w, r, body)
	case "WATCH":
		s.handleWatch(w, r, body)
	default:
		writeError(w, http.StatusMethodNotAllowed, api.NewError("method_not_allowed", fmt.Sprintf("method %s not allowed", r.Method)))
	}
}


// handleMatch handles MATCH requests (reads).
func (s *Server) handleMatch(w http.ResponseWriter, r *http.Request, body *api.RequestBody) {
	// Extract path string from body.Path
	pathStr, err := extractPathString(body.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, fmt.Sprintf("invalid path: %v", err)))
		return
	}

	// Route based on path
	if pathStr == "/api/transactions" {
		s.handleMatchTransaction(w, r, body)
		return
	}
	
	// Validate data path
	if err := validateDataPath(pathStr); err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, err.Error()))
		return
	}
	
	s.handleMatchData(w, r, body)
}

// handlePatch handles PATCH requests (writes).
func (s *Server) handlePatch(w http.ResponseWriter, r *http.Request, body *api.RequestBody) {
	// Extract path string from body.Path
	pathStr, err := extractPathString(body.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, fmt.Sprintf("invalid path: %v", err)))
		return
	}

	// Route based on path
	if pathStr == "/api/transactions" {
		s.handlePatchTransaction(w, r, body)
		return
	}
	
	// Validate data path
	if err := validateDataPath(pathStr); err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, err.Error()))
		return
	}
	
	s.handlePatchData(w, r, body)
}

// handleWatch handles WATCH requests (streaming).
func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request, body *api.RequestBody) {
	// Extract path string from body.Path
	pathStr, err := extractPathString(body.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, fmt.Sprintf("invalid path: %v", err)))
		return
	}

	// Route based on path
	if pathStr == "/api/transactions" {
		s.handleWatchTransaction(w, r, body)
		return
	}
	
	// Validate data path
	if err := validateDataPath(pathStr); err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, err.Error()))
		return
	}
	
	s.handleWatchData(w, r, body)
}

// extractPathString extracts the path string from an ir.Node.
func extractPathString(pathNode *ir.Node) (string, error) {
	if pathNode == nil {
		return "", fmt.Errorf("path is required")
	}
	if pathNode.Type != ir.StringType {
		return "", fmt.Errorf("path must be a string")
	}
	return pathNode.String, nil
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, statusCode int, err *api.Error) {
	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(statusCode)

	// Encode error as Tony document using FromMap to preserve parent pointers
	errorNode := ir.FromMap(map[string]*ir.Node{
		"error": ir.FromMap(map[string]*ir.Node{
			"code":    &ir.Node{Type: ir.StringType, String: err.Code},
			"message": &ir.Node{Type: ir.StringType, String: err.Message},
		}),
	})

	encode.Encode(errorNode, w)
}

// Stub handlers - to be implemented




func (s *Server) handleWatchTransaction(w http.ResponseWriter, r *http.Request, body *api.RequestBody) {
	// TODO: Implement
	writeError(w, http.StatusNotImplemented, api.NewError("not_implemented", "WATCH /api/transactions not yet implemented"))
}

