# spendlint

**A linter for your LLM bill. Pre-merge cost gate for GitLab.**

spendlint reviews every merge request for LLM cost impact before it ships. It reads the diff, finds LLM-touching changes, projects the dollar delta against real historical traffic, and posts a verdict comment on the MR - automatically, on every open and update.

**Live demo:** https://spendlint-m3wqnzwklq-uc.a.run.app

Built for the [Google Cloud Rapid Agent hackathon](https://googlecloudmultiagents.devpost.com/) - GitLab track.

---

## The problem

Observability tools tell you *after* a spike which deploy caused it. The bill has already arrived.

The silent killers all live in a diff:

| Change | Cost impact |
|---|---|
| `claude-haiku` to `claude-sonnet` | ~12x per token |
| Retry loop added around an existing call | volume multiplier |
| `max_tokens` bumped | output cost increase |
| New call site added to a hot path | adds a whole term |

spendlint catches these at review time, not after the invoice.

---

## Example comment (posted automatically on MR open)

Real output from [MR !1](https://gitlab.com/Yatsuiii/spendlint/-/merge_requests/1) — a `gemini-2.5-pro` to `gemini-2.5-flash` swap:

```
**Verdict: PASS**
This change is projected to save $0.0025/day.

| Call Site | Change                            | Baseline $/day | Projected $/day | Delta    |
| default   | gemini-2.5-pro -> gemini-2.5-flash | $0.0033        | $0.0008         | -$0.0025 |

**Assumptions**
no recorded traffic for label ""; showing unit cost (1k in / 200 out):
$1.250/$10.000 per 1M tokens -> $0.300/$2.500 per 1M tokens
```

For a higher-traffic scenario (e.g. a `claude-haiku` to `claude-sonnet` swap at 600 calls/day):

```
**Verdict: WARN** (+$14.23/day)

| Call Site        | Change                             | Baseline $/day | Projected $/day | Delta      |
| summary_endpoint | claude-3-haiku -> claude-3-5-sonnet | $0.45          | $14.68          | +$14.23    |

**Assumptions:** 600 calls/day (30-day avg), 1397 avg input tokens, 319 avg output tokens.
```

---

## How it works

```
GitLab MR opened
        │ webhook
        ▼
  Cloud Run (spendlint)
        │
        ▼
  Gemini 2.5 Pro (Vertex AI) - tool-calling loop
        │
   ┌────┴──────────────────────┐
   ▼                           ▼
get_mr_diff               analyze_diff_cost
(GitLab REST API)         (local projection engine)
                               │
                          SQLite ledger
                          (historical traffic
                           per call-site label)
        │
        ▼
  post_mr_comment
  (GitLab REST API)
```

### The cost projection formula

```
baseline  = calls_per_day * cost(old_model, avg_in_tokens, avg_out_tokens)
projected = calls_per_day * volume_mult * cost(new_model, avg_in_tokens, new_out_tokens)
delta     = projected - baseline
```

The projector classifies each diff hunk into a change type and fills the formula:

| Change type | What it shifts |
|---|---|
| `model_swap` | token rates (from the pricing table) |
| `volume_added` / `volume_removed` | calls per day (3x multiplier) |
| `max_tokens_change` | output token cap |
| `call_added` / `call_removed` | adds or removes a whole cost term |

**Verdicts:** PASS (<$1/day delta) - WARN ($1-10/day) - BLOCK (>$10/day) - INFO (saving money)

---

## Setup

### Prerequisites

- Go 1.24+
- Google Cloud project with Vertex AI API enabled
- GitLab account + personal access token (`api` scope)

### Run locally

```bash
git clone https://github.com/Yatsuiii/spendlint
cd spendlint

# Seed the demo ledger with 30 days of synthetic traffic
go run ./cmd/spendlint seed

# Show per-label cost stats
go run ./cmd/spendlint stats

# Review a local diff against the seeded ledger
git diff main...my-branch | go run ./cmd/spendlint review

# Run the Gemini agent on a real GitLab MR
export GOOGLE_CLOUD_PROJECT=your-gcp-project
export GITLAB_TOKEN=glpat-...
go run ./cmd/spendlint review-mr --project group/repo --mr 42
```

### Deploy to Cloud Run

```bash
export GITLAB_TOKEN=glpat-...
export GITLAB_MCP_URL=https://gitlab.com/api/v4/mcp
bash deploy.sh
```

Wire the webhook in GitLab: **Settings > Webhooks > Add webhook**
- URL: `https://your-service.run.app/webhook`
- Trigger: Merge request events

### Environment variables

| Variable | Description |
|---|---|
| `GOOGLE_CLOUD_PROJECT` | GCP project ID for Vertex AI |
| `GITLAB_TOKEN` | GitLab PAT (`api` scope) |
| `GITLAB_MCP_URL` | GitLab MCP endpoint (default: GitLab.com) |
| `SPENDLINT_DB` | SQLite ledger path (default: `spendlint.db`) |
| `PORT` | HTTP port for `serve` (default: `8080`) |
| `SPENDLINT_RECORD_TOKEN` | Shared secret for `POST /record` (set to enable ingestion) |

---

## Instrumenting your code

Tag each LLM call site with a stable label. spendlint uses this to join the diff to historical traffic in the ledger.

```python
# spendlint:label summary_endpoint
response = anthropic.messages.create(
    model="claude-3-haiku-20240307",
    max_tokens=2048,
    messages=messages,
)
```

```typescript
// spendlint:label chat_assistant
const response = await anthropic.messages.create({
  model: "claude-3-5-sonnet-20241022",
  max_tokens: 1024,
  messages,
});
```

Record calls to the ledger from your Go app:

```go
import "github.com/Yatsuiii/spendlint/pkg/client"

c := client.New("https://your-spendlint.run.app", os.Getenv("SPENDLINT_RECORD_TOKEN"))
c.Record(ctx, client.Call{
    Label:        "summary_endpoint",
    Model:        "gemini-2.5-pro",
    InputTokens:  resp.UsageMetadata.PromptTokenCount,
    OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
})
```

---

## Package layout

```
cmd/spendlint/        CLI: seed, stats, review, review-mr, serve
internal/
  pricing/            per-model token rates (Anthropic, OpenAI, Google)
  ledger/             SQLite store: calls, per-label rollups, review history
  recorder/           middleware to record LLM calls from your app
  diff/               unified diff parser + change-type classifier
  project/            cost-projection engine (the novel core)
  agent/              Gemini 2.5 Pro tool-calling loop (Vertex AI)
  gitlab/             GitLab MCP client (JSON-RPC over mcp-remote stdio)
  web/                webhook receiver + dashboard
  seed/               deterministic demo seeder (30-day synthetic history)
deploy.sh             Cloud Run deploy script
```

---

## Technology

- **Agent brain:** Gemini 2.5 Pro on Vertex AI (`google.golang.org/genai`)
- **GitLab integration:** GitLab MCP server + REST API
- **Deployment:** Google Cloud Run
- **Ledger:** SQLite via `modernc.org/sqlite` (pure Go, no cgo)
- **Language:** Go 1.25

---

## Limitations

- The diff-to-call-site join relies on the `# spendlint:label` convention. Arbitrary codebases with heavily indirected LLM calls require manual labeling.
- Projection assumes current traffic volume holds. Seasonal spikes or ramp-up are not modeled.
- Pricing table is hardcoded; update `internal/pricing/pricing.go` when vendors change rates.

---

## License

MIT. See [LICENSE](LICENSE).
