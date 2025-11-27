package server

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Server represents the logd HTTP server.
type Server struct {
	Config      Config
	txWaitersMu sync.Mutex
	txWaiters   map[string]*transactionWaiter // transactionID -> waiter
}

// New creates a new Server instance.
func New(cfg *Config) *Server {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slogLevel(),
		}))
	}
	return &Server{
		Config:    *cfg,
		txWaiters: make(map[string]*transactionWaiter),
	}
}

func slogLevel() slog.Level {
	if os.Getenv("DEBUG") != "" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

// acquireWaiter gets or creates a waiter for a transaction and increments its reference count.
// The caller must call releaseWaiter when done using the waiter.
func (s *Server) acquireWaiter(transactionID string) *transactionWaiter {
	s.txWaitersMu.Lock()
	defer s.txWaitersMu.Unlock()

	waiter, exists := s.txWaiters[transactionID]
	if !exists {
		waiter = NewTransactionWaiter()
		s.txWaiters[transactionID] = waiter
	}

	// Increment reference count while holding map lock
	waiter.mu.Lock()
	waiter.refCount++
	waiter.mu.Unlock()

	return waiter
}

// releaseWaiter decrements the waiter's reference count and removes it from the map
// if the reference count reaches zero and the transaction is completed.
func (s *Server) releaseWaiter(transactionID string) {
	s.txWaitersMu.Lock()
	defer s.txWaitersMu.Unlock()

	waiter, exists := s.txWaiters[transactionID]
	if !exists {
		return
	}

	waiter.mu.Lock()
	waiter.refCount--
	shouldRemove := waiter.refCount == 0 && waiter.result != nil
	waiter.mu.Unlock()

	// Only remove when no active users AND transaction completed
	if shouldRemove {
		delete(s.txWaiters, transactionID)
	}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only handle /api/data for now
	if r.URL.Path != "/api/data" {
		http.NotFound(w, r)
		return
	}

	// Route based on HTTP method
	switch r.Method {
	case "MATCH":
		d, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("i/o error: %v", err), http.StatusInternalServerError)
			return
		}
		// Parse request body
		req := &api.Match{}
		if err := req.FromTony(d); err != nil {
			writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, fmt.Sprintf("failed to parse request body: %v", err)))
		}
		s.handleMatch(w, r, req)
	case "PATCH":
		// Parse request body
		d, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("i/o error: %v", err), http.StatusInternalServerError)
			return
		}
		req := &api.Patch{}
		if err := req.FromTony(d); err != nil {
			writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, fmt.Sprintf("failed to parse request body: %v", err)))
		}
		s.handlePatch(w, r, req)
	case "WATCH":
		// Parse request body
		body, err := api.ParseRequestBody(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, fmt.Sprintf("failed to parse request body: %v", err)))
			return
		}
		s.handleWatch(w, r, body)
	default:
		writeError(w, http.StatusMethodNotAllowed, api.NewError("method_not_allowed", fmt.Sprintf("method %s not allowed", r.Method)))
	}
}

// handleMatch handles MATCH requests (reads).
func (s *Server) handleMatch(w http.ResponseWriter, r *http.Request, req *api.Match) {
	s.handleMatchData(w, r, req)
}

// handlePatch handles PATCH requests (writes).
func (s *Server) handlePatch(w http.ResponseWriter, r *http.Request, req *api.Patch) {
	s.handlePatchData(w, r, req)
}

// handleWatch handles WATCH requests (streaming).
func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request, body *api.Body) {
	// Validate data path
	if err := validateDataPath(body.Path); err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, err.Error()))
		return
	}

	s.handleWatchData(w, r, body)
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
