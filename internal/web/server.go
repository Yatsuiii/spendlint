// Package web is the HTTP server: a GitLab webhook receiver and a minimal
// read-only dashboard.
package web

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Yatsuiii/spendlint/internal/agent"
	"github.com/Yatsuiii/spendlint/internal/ledger"
)

// Config holds the runtime configuration for the web server.
type Config struct {
	Port          string
	GCPProject    string
	GCPLocation   string
	MCPUrl        string
	WebhookSecret string // optional; if set, X-Gitlab-Token must match
	RecordToken   string // optional; if set, /record requires X-Spendlint-Token
	Ledger        *ledger.Ledger
}

// Server is the HTTP server.
type Server struct {
	cfg Config
	mux *http.ServeMux
}

// New creates a Server and registers all routes.
func New(cfg Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /webhook", s.handleWebhook)
	s.mux.HandleFunc("POST /record", s.handleRecord)
	s.mux.HandleFunc("GET /", s.handleDashboard)
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	return s
}

// ListenAndServe starts the HTTP server. addr may be ":PORT".
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 5 * time.Minute,
	}
	return srv.ListenAndServe()
}

// newAgent creates a fresh Gemini agent for a single review.
func (s *Server) newAgent(ctx context.Context) (*agent.Agent, error) {
	return agent.New(ctx, s.cfg.GCPProject, s.cfg.GCPLocation, s.cfg.MCPUrl, s.cfg.Ledger)
}
