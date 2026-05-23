package gitlab

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// DefaultMCPURL is the GitLab.com MCP server endpoint.
const DefaultMCPURL = "https://gitlab.com/api/v4/mcp"

// StdioClient drives `mcp-remote` as a subprocess and speaks MCP JSON-RPC over
// its stdio. mcp-remote handles the OAuth flow that the GitLab MCP server
// requires (a personal access token is not accepted), so this is the working
// transport; the HTTP client in mcp.go is kept only as a reference.
type StdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	id     int
	mu     sync.Mutex
}

type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// NewStdioClient spawns `npx -y mcp-remote <mcpURL>`. On first run mcp-remote
// opens a browser for GitLab OAuth consent and prints the URL to stderr, which
// is surfaced to the user. The token is cached for subsequent runs.
func NewStdioClient(ctx context.Context, mcpURL string) (*StdioClient, error) {
	if mcpURL == "" {
		mcpURL = DefaultMCPURL
	}
	cmd := exec.CommandContext(ctx, "npx", "-y", "mcp-remote", mcpURL)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mcp-remote (is node/npx installed?): %w", err)
	}
	return &StdioClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}, nil
}

func (c *StdioClient) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = c.stdin.Write(b)
	return err
}

// readResponse reads newline-delimited JSON from stdout until it finds the
// JSON-RPC response with the given id, skipping notifications and log lines.
func (c *StdioClient) readResponse(id int) (json.RawMessage, error) {
	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("read stdout: %w", err)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var rr rpcResponse
		if err := json.Unmarshal(line, &rr); err != nil {
			continue
		}
		if rr.ID != id {
			continue
		}
		if rr.Error != nil {
			return nil, rr.Error
		}
		return rr.Result, nil
	}
}

func (c *StdioClient) call(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id++
	id := c.id
	if err := c.send(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return nil, fmt.Errorf("send %s: %w", method, err)
	}
	return c.readResponse(id)
}

// Initialize performs the MCP handshake: initialize request, then the required
// initialized notification.
func (c *StdioClient) Initialize() (json.RawMessage, error) {
	res, err := c.call("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "spendlint", "version": "0.0.1"},
	})
	if err != nil {
		return nil, err
	}
	if err := c.send(rpcNotification{JSONRPC: "2.0", Method: "notifications/initialized"}); err != nil {
		return nil, fmt.Errorf("send initialized notification: %w", err)
	}
	return res, nil
}

// CallTool invokes an MCP tool by name and returns the raw result.
func (c *StdioClient) CallTool(name string, args map[string]any) (json.RawMessage, error) {
	return c.call("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
}

// Close shuts down the mcp-remote subprocess.
func (c *StdioClient) Close() error {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}

// ListToolsRaw returns the raw tools/list result including full input schemas.
func (c *StdioClient) ListToolsRaw() (json.RawMessage, error) {
	return c.call("tools/list", map[string]any{})
}

// ListTools returns the names of all tools the MCP server exposes.
func (c *StdioClient) ListTools() ([]string, error) {
	raw, err := c.call("tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}
	names := make([]string, len(result.Tools))
	for i, t := range result.Tools {
		names[i] = t.Name
	}
	return names, nil
}

// CheckMCP verifies connectivity through mcp-remote: it opens a session and
// calls get_mcp_server_version, returning the raw server result. The ctx
// deadline should be generous enough to cover a first-run OAuth consent.
func CheckMCP(ctx context.Context) (string, error) {
	c, err := NewStdioClient(ctx, os.Getenv("GITLAB_MCP_URL"))
	if err != nil {
		return "", err
	}
	defer c.Close()
	if _, err := c.Initialize(); err != nil {
		return "", fmt.Errorf("initialize session: %w", err)
	}
	res, err := c.CallTool("get_mcp_server_version", map[string]any{})
	if err != nil {
		return "", fmt.Errorf("call get_mcp_server_version: %w", err)
	}
	return string(res), nil
}
