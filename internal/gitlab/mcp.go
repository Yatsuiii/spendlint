// Package gitlab is a minimal client for the GitLab MCP server.
//
// It speaks JSON-RPC 2.0 over the MCP streamable-HTTP transport: each call is a
// POST to the configured endpoint, and the server replies with either
// application/json or a text/event-stream whose payload rides on data: lines.
package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Client talks to a GitLab MCP server.
type Client struct {
	endpoint string
	token    string
	http     *http.Client
	id       int
}

// NewClientFromEnv builds a client from GITLAB_MCP_URL and GITLAB_TOKEN.
//
// GITLAB_MCP_URL is the MCP server endpoint (confirm the exact URL from the
// GitLab MCP docs for your instance). GITLAB_TOKEN is a GitLab personal access
// token with api scope.
func NewClientFromEnv() (*Client, error) {
	ep := os.Getenv("GITLAB_MCP_URL")
	if ep == "" {
		return nil, fmt.Errorf("GITLAB_MCP_URL is not set (confirm the MCP endpoint from GitLab's docs)")
	}
	tok := os.Getenv("GITLAB_TOKEN")
	if tok == "" {
		return nil, fmt.Errorf("GITLAB_TOKEN is not set (create a GitLab personal access token with api scope)")
	}
	return &Client{
		endpoint: ep,
		token:    tok,
		http:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("mcp rpc error %d: %s", e.Code, e.Message) }

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.id++
	reqBody, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: c.id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post to mcp server: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("mcp server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	payload := extractJSON(resp.Header.Get("Content-Type"), body)
	var rr rpcResponse
	if err := json.Unmarshal(payload, &rr); err != nil {
		return nil, fmt.Errorf("decode response: %w (raw: %s)", err, strings.TrimSpace(string(payload)))
	}
	if rr.Error != nil {
		return nil, rr.Error
	}
	return rr.Result, nil
}

// extractJSON pulls the JSON-RPC payload out of either a plain JSON body or an
// SSE stream, where the last data: line carries the response.
func extractJSON(contentType string, body []byte) []byte {
	if !strings.Contains(contentType, "text/event-stream") {
		return body
	}
	var last []byte
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "data:"); ok {
			last = []byte(strings.TrimSpace(after))
		}
	}
	if last == nil {
		return body
	}
	return last
}

// Initialize opens an MCP session.
func (c *Client) Initialize(ctx context.Context) error {
	_, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "spendlint",
			"version": "0.0.1",
		},
	})
	return err
}

// CallTool invokes an MCP tool by name and returns the raw result.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	return c.call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
}

// Note: connectivity verification (CheckMCP) lives in mcp_stdio.go, which uses
// the mcp-remote OAuth transport. The HTTP client above is a reference only;
// the GitLab MCP endpoint rejects personal access tokens.
