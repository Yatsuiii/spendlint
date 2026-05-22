# spendlint

**A linter for your LLM bill.** spendlint reviews every GitLab merge request for LLM cost impact before it ships, reads the diff, projects the spend delta against your real traffic, and comments on the MR with a verdict.

![status](https://img.shields.io/badge/status-active%20development-orange) ![go](https://img.shields.io/badge/go-1.26-00ADD8?logo=go) ![license](https://img.shields.io/badge/license-MIT-blue)

```
spendlint review !42

reviewing MR !42 "switch summary endpoint to claude-sonnet"

  diff touches 1 LLM call site: summary_endpoint
  change: model claude-haiku -> claude-sonnet

  baseline   $4.68/day   (3,000 calls/day · 91% haiku)
  projected  $19.20/day  (same volume · 89% sonnet)
  delta      +$14.52/day  (+310%)

  verdict: BLOCK. This MR would more than quadruple spend on this
  call site at current traffic. Confirm the quality gain justifies it.
```

---

## The problem

Every team shipping AI features knows the after-the-fact version of this: the bill arrives, spend doubled, and someone spends an afternoon working out which deploy did it. Tools exist for that postmortem.

Nobody catches it at code review. A one-line model swap in an MR, a retry loop added on top of a call, a bumped `max_tokens`, all of these ship silently and only show up on next month's invoice.

spendlint moves the check left. It reads the merge request diff, finds the code that touches LLM calls, projects the cost delta against your historical traffic, and posts the number on the MR before it merges.

## How it works

```
GitLab MR opened ──webhook──▶ spendlint (Cloud Run)
                                   │
                    ┌──────────────┼───────────────┐
                    ▼              ▼                ▼
            GitLab MCP        Gemini agent      cost ledger
        (get diffs,          (Vertex AI,        (calls · cost ·
         semantic search,     tool-calling)      label · tokens)
         post MR comment)
                    │
                    ▼
        MR comment with projected $/day delta, reason, and verdict
```

## Status

In active development for the Google Cloud Rapid Agent hackathon (GitLab track). Built with Gemini on Vertex AI and the GitLab MCP server, deployed on Cloud Run.

## License

MIT.
