package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

var dashTmpl = template.Must(template.New("dash").Funcs(template.FuncMap{
	"fmtDelta": func(d float64) string {
		if d >= 0 {
			return fmt.Sprintf("+$%.4f/day", d)
		}
		return fmt.Sprintf("-$%.4f/day", -d)
	},
	"verdictClass": func(v string) string {
		switch v {
		case "BLOCK":
			return "block"
		case "WARN":
			return "warn"
		case "PASS":
			return "pass"
		default:
			return "info"
		}
	},
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>spendlint</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: ui-monospace, monospace; background: #0d1117; color: #e6edf3; padding: 2rem; }
h1 { font-size: 1.4rem; margin-bottom: 0.25rem; color: #58a6ff; }
.subtitle { color: #8b949e; font-size: 0.85rem; margin-bottom: 2rem; }
h2 { font-size: 1rem; color: #8b949e; margin: 1.5rem 0 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
th { text-align: left; color: #8b949e; padding: 0.4rem 0.75rem; border-bottom: 1px solid #21262d; }
td { padding: 0.4rem 0.75rem; border-bottom: 1px solid #161b22; }
tr:hover td { background: #161b22; }
.block { color: #f85149; font-weight: bold; }
.warn  { color: #e3b341; }
.pass  { color: #3fb950; }
.info  { color: #58a6ff; }
.pill { display: inline-block; padding: 0.1rem 0.5rem; border-radius: 2rem; font-size: 0.75rem; }
.pill.block { background: #3d1a1a; }
.pill.warn  { background: #3d2d0e; }
.pill.pass  { background: #0d2e1a; }
.pill.info  { background: #0d1f3d; }
a { color: #58a6ff; text-decoration: none; }
a:hover { text-decoration: underline; }
</style>
</head>
<body>
<h1>spendlint</h1>
<p class="subtitle">pre-merge LLM cost gate - a linter for your LLM bill</p>

<h2>Recent Reviews</h2>
{{if .Reviews}}
<table>
<tr><th>MR</th><th>Project</th><th>Title</th><th>Verdict</th><th>Delta</th><th>Time</th></tr>
{{range .Reviews}}
<tr>
  <td><a href="https://gitlab.com/{{.Project}}/-/merge_requests/{{.MRIID}}" target="_blank">!{{.MRIID}}</a></td>
  <td>{{.Project}}</td>
  <td>{{.MRTitle}}</td>
  <td><span class="pill {{verdictClass .Verdict}} {{verdictClass .Verdict}}">{{.Verdict}}</span></td>
  <td>{{fmtDelta .DeltaDay}}</td>
  <td>{{.Timestamp.Format "2006-01-02 15:04"}}</td>
</tr>
{{end}}
</table>
{{else}}
<p style="color:#8b949e;font-size:0.85rem">No reviews yet. Configure the GitLab webhook to point at <code>/webhook</code>.</p>
{{end}}

<h2>Call-Site Traffic (Ledger)</h2>
{{if .Stats}}
<table>
<tr><th>Label</th><th>Model</th><th>Calls/day</th><th>Avg In tok</th><th>Avg Out tok</th><th>$/day</th></tr>
{{range .Stats}}
<tr>
  <td>{{.Label}}</td>
  <td>{{.DominantModel}}</td>
  <td>{{printf "%.1f" .CallsPerDay}}</td>
  <td>{{printf "%.0f" .AvgInTokens}}</td>
  <td>{{printf "%.0f" .AvgOutTokens}}</td>
  <td>{{printf "$%.4f" .CostPerDayUSD}}</td>
</tr>
{{end}}
</table>
{{else}}
<p style="color:#8b949e;font-size:0.85rem">No calls recorded yet. Route LLM traffic through spendlint to populate the ledger.</p>
{{end}}

<p style="margin-top:2rem;color:#8b949e;font-size:0.75rem">
  spendlint - <a href="https://github.com/Yatsuiii/spendlint" target="_blank">source</a>
  - built for the Google Cloud Rapid Agent hackathon (GitLab track)
</p>
</body>
</html>
`))

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	reviews, err := s.cfg.Ledger.RecentReviews(20)
	if err != nil {
		http.Error(w, "ledger error", http.StatusInternalServerError)
		return
	}
	stats, err := s.cfg.Ledger.Stats()
	if err != nil {
		http.Error(w, "ledger error", http.StatusInternalServerError)
		return
	}

	data := struct {
		Reviews interface{}
		Stats   interface{}
	}{reviews, stats}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashTmpl.Execute(w, data); err != nil {
		// Template already started writing; log and bail.
		_ = strings.Contains(err.Error(), "") // suppress unused import
	}
}
