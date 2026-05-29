package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Yatsuiii/spendlint/internal/ledger"
	"github.com/Yatsuiii/spendlint/internal/pricing"
	"github.com/Yatsuiii/spendlint/internal/recorder"
)

type recordReq struct {
	Label        string    `json:"label"`
	Model        string    `json:"model"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	PromptHash   string    `json:"prompt_hash,omitempty"`
	Prompt       string    `json:"prompt,omitempty"`
	Timestamp    time.Time `json:"timestamp,omitempty"`
}

type recordResp struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	CostUSD   float64   `json:"cost_usd"`
	Provider  string    `json:"provider"`
}

func (s *Server) handleRecord(w http.ResponseWriter, r *http.Request) {
	if s.cfg.RecordToken != "" {
		got := r.Header.Get("X-Spendlint-Token")
		if got == "" {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				got = strings.TrimPrefix(h, "Bearer ")
			}
		}
		if got != s.cfg.RecordToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var req recordReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Label == "" || req.Model == "" {
		http.Error(w, "label and model required", http.StatusBadRequest)
		return
	}
	if req.InputTokens < 0 || req.OutputTokens < 0 {
		http.Error(w, "tokens must be non-negative", http.StatusBadRequest)
		return
	}

	hash := req.PromptHash
	if hash == "" && req.Prompt != "" {
		hash = recorder.PromptHash(req.Prompt)
	}
	ts := req.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	rate, known := pricing.Lookup(req.Model)
	provider := ""
	cost := 0.0
	if known {
		provider = rate.Provider
		cost = pricing.CostFor(rate, req.InputTokens, req.OutputTokens)
	}
	call := ledger.Call{
		Timestamp:    ts,
		Label:        req.Label,
		Provider:     provider,
		Model:        req.Model,
		InputTokens:  req.InputTokens,
		OutputTokens: req.OutputTokens,
		CostUSD:      cost,
		PromptHash:   hash,
	}
	if err := s.cfg.Ledger.Record(&call); err != nil {
		http.Error(w, "ledger error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(recordResp{
		ID:        call.ID,
		Timestamp: call.Timestamp,
		CostUSD:   call.CostUSD,
		Provider:  call.Provider,
	})
}
