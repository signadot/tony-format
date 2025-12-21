package server

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Server represents the logd HTTP server.
type Server struct {
	Config Config
}

// New creates a new Server instance.
func New(cfg *Config) *Server {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slogLevel(),
		}))
	}
	return &Server{
		Config: *cfg,
	}
}

func slogLevel() slog.Level {
	if os.Getenv("DEBUG") != "" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
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
		req := &api.Match{}
		if err := req.FromTony(d); err != nil {
			writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, fmt.Sprintf("failed to parse request body: %v", err)))
			return
		}
		s.handleMatch(w, r, req)
	case "PATCH":
		d, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("i/o error: %v", err), http.StatusInternalServerError)
			return
		}
		req := &api.Patch{}
		if err := req.FromTony(d); err != nil {
			writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, fmt.Sprintf("failed to parse request body: %v", err)))
			return
		}
		s.handlePatch(w, r, req)
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

// writeError writes an error response.
func writeError(w http.ResponseWriter, statusCode int, err *api.Error) {
	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(statusCode)

	errorNode := ir.FromMap(map[string]*ir.Node{
		"error": ir.FromMap(map[string]*ir.Node{
			"code":    &ir.Node{Type: ir.StringType, String: err.Code},
			"message": &ir.Node{Type: ir.StringType, String: err.Message},
		}),
	})

	encode.Encode(errorNode, w)
}
