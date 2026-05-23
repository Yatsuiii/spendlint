// Package agent is the Gemini tool-calling loop. It connects a Vertex AI
// Gemini model to two tool backends: the GitLab MCP server (for diffs and
// comment posting) and the local projection engine (for cost math). Gemini
// drives the review: it decides which tools to call, interprets the results,
// and composes the MR comment.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"google.golang.org/genai"

	"github.com/Yatsuiii/spendlint/internal/gitlab"
	"github.com/Yatsuiii/spendlint/internal/ledger"
)

const defaultModel = "gemini-2.5-pro"
const maxToolTurns = 10

// Agent holds the Gemini client, MCP transport, and local tools.
type Agent struct {
	gemini *genai.Client
	model  string
	led    *ledger.Ledger
	mcpURL string
}

// New creates an Agent. gcpProject and gcpLocation configure Vertex AI;
// mcpURL is the GitLab MCP endpoint (defaults to GitLab.com).
func New(ctx context.Context, gcpProject, gcpLocation, mcpURL string, led *ledger.Ledger) (*Agent, error) {
	c, err := genai.NewClient(ctx, &genai.ClientConfig{
		Project:  gcpProject,
		Location: gcpLocation,
		Backend:  genai.BackendVertexAI,
	})
	if err != nil {
		return nil, fmt.Errorf("new genai client: %w", err)
	}
	if mcpURL == "" {
		mcpURL = os.Getenv("GITLAB_MCP_URL")
	}
	return &Agent{gemini: c, model: defaultModel, led: led, mcpURL: mcpURL}, nil
}

// ReviewMR runs the agent loop for one merge request. It returns the comment
// text Gemini composed, and posts it to the MR via the GitLab MCP server.
func (a *Agent) ReviewMR(ctx context.Context, projectPath string, mrIID int) (string, error) {
	// Only start mcp-remote when we have no GITLAB_TOKEN for REST fallback.
	// In Cloud Run (or any headless env) mcp-remote needs OAuth browser consent
	// which is unavailable; the REST API path covers get/diff/comment instead.
	var mcp *gitlab.StdioClient
	if os.Getenv("GITLAB_TOKEN") == "" {
		var err error
		mcp, err = gitlab.NewStdioClient(ctx, a.mcpURL)
		if err != nil {
			return "", fmt.Errorf("start mcp-remote: %w", err)
		}
		defer mcp.Close()
		if _, err := mcp.Initialize(); err != nil {
			return "", fmt.Errorf("mcp initialize: %w", err)
		}
	}

	tools := buildTools()
	th := &toolHandler{mcp: mcp, led: a.led, projectPath: projectPath, mrIID: mrIID}
	config := &genai.GenerateContentConfig{
		Tools: tools,
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{{Text: systemPrompt}},
		},
	}

	contents := []*genai.Content{{
		Role: "user",
		Parts: []*genai.Part{{Text: fmt.Sprintf(
			"Review merge request !%d in GitLab project %q for LLM cost impact. "+
				"Get the diff, analyze it for cost changes, and post a detailed review comment.",
			mrIID, projectPath,
		)}},
	}}

	for turn := 0; turn < maxToolTurns; turn++ {
		resp, err := a.gemini.Models.GenerateContent(ctx, a.model, contents, config)
		if err != nil {
			return "", fmt.Errorf("gemini turn %d: %w", turn, err)
		}
		if len(resp.Candidates) == 0 {
			return "", fmt.Errorf("gemini returned no candidates on turn %d", turn)
		}
		candidate := resp.Candidates[0]
		if candidate.Content == nil {
			return "", fmt.Errorf("gemini returned nil content on turn %d", turn)
		}

		// Append model response to conversation.
		contents = append(contents, candidate.Content)

		// Collect function calls from all parts.
		var calls []*genai.FunctionCall
		var finalText string
		for _, part := range candidate.Content.Parts {
			if part.FunctionCall != nil {
				calls = append(calls, part.FunctionCall)
			}
			if part.Text != "" {
				finalText = part.Text
			}
		}

		// If no tool calls, Gemini is done - return its text.
		if len(calls) == 0 {
			return finalText, nil
		}

		// Execute each tool call and collect results.
		var responseParts []*genai.Part
		for _, fc := range calls {
			result, execErr := th.dispatch(ctx, fc.Name, fc.Args)
			response := map[string]any{"output": result}
			if execErr != nil {
				response = map[string]any{"error": execErr.Error()}
			}
			responseParts = append(responseParts, &genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					ID:       fc.ID,
					Name:     fc.Name,
					Response: response,
				},
			})
		}
		contents = append(contents, &genai.Content{
			Role:  "user",
			Parts: responseParts,
		})
	}
	return "", fmt.Errorf("agent exceeded %d tool turns without finishing", maxToolTurns)
}

// buildTools returns the tool declarations Gemini can call.
func buildTools() []*genai.Tool {
	strProp := func(desc string) *genai.Schema {
		return &genai.Schema{Type: genai.TypeString, Description: desc}
	}
	intProp := func(desc string) *genai.Schema {
		return &genai.Schema{Type: genai.TypeInteger, Description: desc}
	}
	return []*genai.Tool{{
		FunctionDeclarations: []*genai.FunctionDeclaration{
			{
				Name:        "get_mr_info",
				Description: "Get metadata for a GitLab merge request (title, description, author, state).",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"project_path": strProp("GitLab project path, e.g. group/repo"),
						"mr_iid":       intProp("Merge request IID"),
					},
					Required: []string{"project_path", "mr_iid"},
				},
			},
			{
				Name:        "get_mr_diff",
				Description: "Get the unified diff for a GitLab merge request.",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"project_path": strProp("GitLab project path"),
						"mr_iid":       intProp("Merge request IID"),
					},
					Required: []string{"project_path", "mr_iid"},
				},
			},
			{
				Name:        "analyze_diff_cost",
				Description: "Parse a unified diff and project the LLM cost impact using the ledger. Returns a JSON summary of changes and dollar deltas.",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"diff": strProp("Unified diff text to analyze"),
					},
					Required: []string{"diff"},
				},
			},
			{
				Name:        "post_mr_comment",
				Description: "Post a review comment on a GitLab merge request.",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"project_path": strProp("GitLab project path"),
						"mr_iid":       intProp("Merge request IID"),
						"body":         strProp("Comment body in Markdown"),
					},
					Required: []string{"project_path", "mr_iid", "body"},
				},
			},
		},
	}}
}

// extractMCPText pulls the first text content block out of an MCP tool result.
// The MCP result envelope is {"content":[{"type":"text","text":"..."}],...}.
func extractMCPText(raw json.RawMessage) string {
	var env struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool   `json:"isError"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return string(raw)
	}
	if env.IsError && env.Error != "" {
		return fmt.Sprintf("error: %s", env.Error)
	}
	for _, c := range env.Content {
		if c.Type == "text" && c.Text != "" {
			return c.Text
		}
	}
	return string(raw)
}

const systemPrompt = `You are spendlint, a GitLab code-review agent that catches LLM cost regressions before they merge.

Your job for every review:
1. Call get_mr_diff to fetch the diff.
2. Call analyze_diff_cost with the full diff text to get the cost projection.
3. Use the projection to compose a clear, actionable Markdown review comment. The comment must include:
   - A verdict line: PASS / WARN / BLOCK with the $/day delta
   - A table of changed call sites with: call site, change type, baseline $/day, projected $/day, delta
   - The assumptions used (token averages, call volume)
   - Concrete advice if the delta is large (e.g. "consider keeping haiku for this path")
4. Call post_mr_comment to post the comment.
5. Respond with a one-sentence summary of what you found.

Be concise in the comment. Show the math. Do not pad with filler text.`
