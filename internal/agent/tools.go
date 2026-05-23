package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	diffpkg "github.com/Yatsuiii/spendlint/internal/diff"
	"github.com/Yatsuiii/spendlint/internal/gitlab"
	"github.com/Yatsuiii/spendlint/internal/ledger"
	"github.com/Yatsuiii/spendlint/internal/project"
)

// toolHandler executes the functions Gemini requests.
type toolHandler struct {
	mcp         *gitlab.StdioClient
	led         *ledger.Ledger
	projectPath string // default project for convenience
	mrIID       int    // default MR IID
}

// dispatch routes a function name + args to the right implementation.
func (h *toolHandler) dispatch(ctx context.Context, name string, args map[string]any) (string, error) {
	result, err := h.dispatchInner(ctx, name, args)
	fmt.Fprintf(os.Stderr, "[tool] %s args=%v -> err=%v result=%.300s\n", name, args, err, result)
	return result, err
}

func (h *toolHandler) dispatchInner(ctx context.Context, name string, args map[string]any) (string, error) {
	switch name {
	case "get_mr_info":
		return h.getMRInfo(ctx, args)
	case "get_mr_diff":
		return h.getMRDiff(ctx, args)
	case "analyze_diff_cost":
		return h.analyzeDiffCost(args)
	case "post_mr_comment":
		return h.postMRComment(ctx, args)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func (h *toolHandler) getMRInfo(ctx context.Context, args map[string]any) (string, error) {
	proj, iid := h.resolveProjectMR(args)
	// Prefer REST API when a token is available (avoids mcp-remote OAuth in Cloud Run).
	if token := os.Getenv("GITLAB_TOKEN"); token != "" {
		return gitlabRESTGet(ctx, token, fmt.Sprintf("projects/%s/merge_requests/%d", strings.ReplaceAll(proj, "/", "%2F"), iid))
	}
	raw, err := h.mcp.CallTool("get_merge_request", map[string]any{
		"id":                proj,
		"merge_request_iid": iid,
	})
	if err != nil {
		return "", fmt.Errorf("get_merge_request: %w", err)
	}
	return extractMCPText(raw), nil
}

func (h *toolHandler) getMRDiff(ctx context.Context, args map[string]any) (string, error) {
	proj, iid := h.resolveProjectMR(args)
	// Prefer REST API when a token is available.
	if token := os.Getenv("GITLAB_TOKEN"); token != "" {
		return gitlabRESTGet(ctx, token, fmt.Sprintf("projects/%s/merge_requests/%d/diffs", strings.ReplaceAll(proj, "/", "%2F"), iid))
	}
	raw, err := h.mcp.CallTool("get_merge_request_diffs", map[string]any{
		"id":                proj,
		"merge_request_iid": iid,
	})
	if err != nil {
		return "", fmt.Errorf("get_merge_request_diffs: %w", err)
	}
	return extractMCPText(raw), nil
}

// gitlabRESTGet fetches a GitLab REST API path and returns the response body.
func gitlabRESTGet(ctx context.Context, token, path string) (string, error) {
	url := "https://gitlab.com/api/v4/" + path
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gitlab REST GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("gitlab REST GET %s: %d %s", path, resp.StatusCode, string(body))
	}
	return string(body), nil
}

// analyzeDiffCost runs the local classifier + projector and serialises results to JSON.
func (h *toolHandler) analyzeDiffCost(args map[string]any) (string, error) {
	diffText, _ := args["diff"].(string)
	if diffText == "" {
		return "", fmt.Errorf("diff arg is empty")
	}
	// GitLab MCP returns an array of {diff, new_path, old_path, ...} objects.
	// Reconstruct a standard unified diff the parser understands.
	if strings.HasPrefix(strings.TrimSpace(diffText), "[") {
		diffText = extractUnifiedDiff(diffText)
	}
	hunks, err := diffpkg.Parse(diffText)
	if err != nil {
		return "", fmt.Errorf("parse diff: %w", err)
	}
	changes := diffpkg.ClassifyAll(hunks)
	if len(changes) == 0 {
		return `{"verdict":"PASS","total_delta_day":0,"changes":[],"note":"No cost-relevant patterns detected in the diff."}`, nil
	}
	proj := project.New(h.led)
	results, err := proj.Project(changes)
	if err != nil {
		return "", fmt.Errorf("project: %w", err)
	}
	total := project.TotalDelta(results)
	verdict := project.Verdict(total)

	type changeJSON struct {
		Label           string  `json:"label"`
		ChangeType      string  `json:"change_type"`
		OldValue        string  `json:"old_value"`
		NewValue        string  `json:"new_value"`
		BaselineDayUSD  float64 `json:"baseline_day_usd"`
		ProjectedDayUSD float64 `json:"projected_day_usd"`
		DeltaDayUSD     float64 `json:"delta_day_usd"`
		Confidence      string  `json:"confidence"`
		Assumption      string  `json:"assumption"`
	}
	var out []changeJSON
	for _, r := range results {
		out = append(out, changeJSON{
			Label:           r.Label,
			ChangeType:      r.ChangeType.String(),
			OldValue:        r.OldValue,
			NewValue:        r.NewValue,
			BaselineDayUSD:  r.BaselineDayUSD,
			ProjectedDayUSD: r.ProjectedDayUSD,
			DeltaDayUSD:     r.DeltaDayUSD,
			Confidence:      r.Confidence,
			Assumption:      r.Assumption,
		})
	}
	summary := struct {
		Verdict       string       `json:"verdict"`
		TotalDeltaDay float64      `json:"total_delta_day"`
		Changes       []changeJSON `json:"changes"`
	}{
		Verdict:       strings.Split(verdict, " - ")[0],
		TotalDeltaDay: total,
		Changes:       out,
	}
	b, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (h *toolHandler) postMRComment(ctx context.Context, args map[string]any) (string, error) {
	proj, iid := h.resolveProjectMR(args)
	body, _ := args["body"].(string)
	if body == "" {
		return "", fmt.Errorf("body arg is empty")
	}
	// The GitLab MCP server has no MR note tool; post via REST API using GITLAB_TOKEN.
	token := os.Getenv("GITLAB_TOKEN")
	if token == "" {
		return "", fmt.Errorf("GITLAB_TOKEN not set; cannot post MR comment")
	}
	encoded := strings.ReplaceAll(proj, "/", "%2F")
	url := fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/merge_requests/%d/notes", encoded, iid)
	payload, _ := json.Marshal(map[string]string{"body": body})
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("post note: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("gitlab notes API %d: %s", resp.StatusCode, string(respBody))
	}
	return fmt.Sprintf("Comment posted to %s/-/merge_requests/%d", proj, iid), nil
}

// resolveProjectMR extracts project_path and mr_iid from args, falling back
// to the values baked in at construction time.
func (h *toolHandler) resolveProjectMR(args map[string]any) (string, int) {
	proj := h.projectPath
	if s, ok := args["project_path"].(string); ok && s != "" {
		proj = s
	}
	iid := h.mrIID
	switch v := args["mr_iid"].(type) {
	case float64:
		iid = int(v)
	case int:
		iid = v
	}
	return proj, iid
}

// extractUnifiedDiff converts GitLab's JSON diff array into a standard unified diff.
// Each element has {"diff":"@@...","new_path":"...","old_path":"..."}.
func extractUnifiedDiff(raw string) string {
	var files []struct {
		Diff    string `json:"diff"`
		NewPath string `json:"new_path"`
		OldPath string `json:"old_path"`
	}
	if err := json.Unmarshal([]byte(raw), &files); err != nil {
		return raw // not parseable, return as-is
	}
	var sb strings.Builder
	for _, f := range files {
		if f.Diff == "" {
			continue
		}
		fmt.Fprintf(&sb, "--- a/%s\n+++ b/%s\n%s\n", f.OldPath, f.NewPath, f.Diff)
	}
	return sb.String()
}
