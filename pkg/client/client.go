// Package client is a thin Go client for the spendlint POST /record endpoint.
// Embed it at LLM call sites in instrumented applications to log every call
// to the spendlint ledger, keyed by a stable call-site label.
package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client posts ledger entries to a spendlint server.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New returns a Client. baseURL is the spendlint base (e.g. https://spendlint.example.com).
// token is the shared secret (X-Spendlint-Token); empty disables auth.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Call is what the caller hands the Client. Prompt is hashed locally so the
// raw text never leaves the caller. Provide either Prompt or PromptHash.
type Call struct {
	Label        string
	Model        string
	InputTokens  int
	OutputTokens int
	Prompt       string
	PromptHash   string
	Timestamp    time.Time
}

// Result is what the server returns for a recorded call.
type Result struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	CostUSD   float64   `json:"cost_usd"`
	Provider  string    `json:"provider"`
}

type recordReq struct {
	Label        string    `json:"label"`
	Model        string    `json:"model"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	PromptHash   string    `json:"prompt_hash,omitempty"`
	Timestamp    time.Time `json:"timestamp,omitempty"`
}

// Record posts one call to the spendlint ledger and returns the server's record.
func (c *Client) Record(ctx context.Context, call Call) (Result, error) {
	if call.Label == "" || call.Model == "" {
		return Result{}, fmt.Errorf("client.Record: label and model required")
	}
	hash := call.PromptHash
	if hash == "" && call.Prompt != "" {
		hash = HashPrompt(call.Prompt)
	}
	body, err := json.Marshal(recordReq{
		Label:        call.Label,
		Model:        call.Model,
		InputTokens:  call.InputTokens,
		OutputTokens: call.OutputTokens,
		PromptHash:   hash,
		Timestamp:    call.Timestamp,
	})
	if err != nil {
		return Result{}, fmt.Errorf("client.Record: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/record", bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("client.Record: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("X-Spendlint-Token", c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("client.Record: http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(resp.Body)
		return Result{}, fmt.Errorf("client.Record: %s: %s", resp.Status, strings.TrimSpace(string(buf)))
	}
	var out Result
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, fmt.Errorf("client.Record: decode response: %w", err)
	}
	return out, nil
}

// HashPrompt returns the short stable digest spendlint stores. Use this if you
// want to compute the hash yourself (e.g. to keep prompts out of process memory
// once hashed). Matches internal/recorder.PromptHash.
func HashPrompt(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:8])
}
