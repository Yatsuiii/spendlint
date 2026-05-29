package web

import (
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/Yatsuiii/spendlint/internal/ledger"
)

type dashStats struct {
	ReviewCount       int
	BlockCount        int
	WarnCount         int
	SavingCount       int
	FlaggedPerDay     float64
	SavedPerDay       float64
	TotalLedgerPerDay float64
}

func computeDashStats(reviews []ledger.Review, sites []ledger.SiteStats) dashStats {
	var s dashStats
	s.ReviewCount = len(reviews)
	for _, r := range reviews {
		switch r.Verdict {
		case "BLOCK":
			s.BlockCount++
		case "WARN":
			s.WarnCount++
		}
		if r.DeltaDay > 0 {
			s.FlaggedPerDay += r.DeltaDay
		} else if r.DeltaDay < 0 {
			s.SavedPerDay += -r.DeltaDay
			s.SavingCount++
		}
	}
	for _, st := range sites {
		s.TotalLedgerPerDay += st.CostPerDayUSD
	}
	return s
}

var dashTmpl = template.Must(template.New("dash").Funcs(template.FuncMap{
	"fmtDelta": func(d float64) string {
		if d >= 0 {
			return fmt.Sprintf("+$%.4f", d)
		}
		return fmt.Sprintf("-$%.4f", -d)
	},
	"deltaSign": func(d float64) string {
		if d > 0 {
			return "up"
		}
		if d < 0 {
			return "down"
		}
		return "flat"
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
	"barWidth": func(v, max float64) string {
		if max <= 0 {
			return "0%"
		}
		pct := v / max * 100
		if pct < 2 && v > 0 {
			pct = 2
		}
		if pct > 100 {
			pct = 100
		}
		return fmt.Sprintf("%.1f%%", pct)
	},
}).Parse(dashHTML))

const dashHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>spendlint &mdash; a linter for your LLM bill</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=Fraunces:ital,wght@1,400;1,500;1,600&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
<style>
:root {
  --sky-1:     #1a1230;
  --sky-2:     #3a1d4a;
  --sky-3:     #7a3458;
  --sky-4:     #c25638;
  --horizon:   #ea8b4b;
  --sun:       #f4c878;
  --ridge-1:   #0d0820;
  --ridge-2:   #1f1432;
  --ridge-3:   #3d2243;
  --ink:       #faf3e4;
  --ink-dim:   #d8c6a6;
  --ink-faint: #957f63;
  --glass:     rgba(20, 12, 28, 0.52);
  --glass-2:   rgba(34, 20, 42, 0.45);
  --glass-edge:rgba(250, 220, 180, 0.14);
  --glass-edge-strong: rgba(250, 220, 180, 0.28);
  --amber:     #f3ad5a;
  --red:       #e07060;
  --green:     #9bbf6e;
  --teal:      #6fb3a8;
  --sans: 'Inter', -apple-system, BlinkMacSystemFont, system-ui, sans-serif;
  --serif: 'Fraunces', 'Times New Roman', serif;
  --mono: 'JetBrains Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
}
* { box-sizing: border-box; margin: 0; padding: 0; }
html, body { color: var(--ink); font-family: var(--sans); font-size: 14px; line-height: 1.5; -webkit-font-smoothing: antialiased; }
body {
  min-height: 100vh;
  background:
    /* sun bloom, lower right */
    radial-gradient(ellipse 60% 38% at 76% 88%, rgba(244,200,120,0.55) 0%, transparent 60%),
    /* horizon glow */
    radial-gradient(ellipse 100% 50% at 50% 110%, rgba(234,139,75,0.78) 0%, transparent 65%),
    /* sky gradient top to bottom */
    linear-gradient(180deg, var(--sky-1) 0%, var(--sky-2) 28%, var(--sky-3) 56%, var(--sky-4) 84%, var(--horizon) 100%);
  background-attachment: fixed;
}
::selection { background: var(--amber); color: #1a0e26; }
a { color: var(--ink); text-decoration: none; }
a:hover { color: var(--amber); }

/* layered horizon silhouettes — 3 ridge layers for depth */
.terrain {
  position: fixed; left: 0; right: 0; bottom: 0; pointer-events: none; z-index: 1;
  height: 75vh;
  background-repeat: no-repeat;
  background-position: 50% 100%, 50% 100%, 50% 100%;
  background-size: 100% 32%, 100% 24%, 100% 18%;
  background-image:
    /* near (darkest, foreground) */
    url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 1600 320' preserveAspectRatio='none'><path fill='%230d0820' d='M0,320 L0,210 L70,168 L150,200 L220,150 L300,180 L380,120 L460,170 L540,140 L640,90 L740,150 L820,110 L920,165 L1000,130 L1080,175 L1180,140 L1280,185 L1360,150 L1460,195 L1540,170 L1600,200 L1600,320 Z'/></svg>"),
    /* mid */
    url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 1600 240' preserveAspectRatio='none'><path fill='%231f1432' fill-opacity='0.92' d='M0,240 L0,150 L80,120 L180,140 L260,90 L360,130 L450,80 L560,120 L660,70 L760,110 L860,75 L960,115 L1050,85 L1160,125 L1260,90 L1360,130 L1480,100 L1600,140 L1600,240 Z'/></svg>"),
    /* far */
    url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 1600 180' preserveAspectRatio='none'><path fill='%233d2243' fill-opacity='0.65' d='M0,180 L0,110 L100,90 L200,105 L320,75 L440,100 L560,80 L680,95 L800,70 L920,90 L1040,72 L1180,98 L1320,82 L1440,100 L1600,88 L1600,180 Z'/></svg>");
}

/* sun disc (subtle) */
.sun {
  position: fixed; right: 12%; bottom: 28vh; width: 220px; height: 220px; border-radius: 50%;
  background: radial-gradient(circle at 50% 50%, rgba(252,232,170,0.95) 0%, rgba(244,200,120,0.55) 30%, rgba(244,200,120,0.0) 70%);
  filter: blur(2px); pointer-events: none; z-index: 1;
  box-shadow: 0 0 200px 60px rgba(244,200,120,0.18);
}

/* grain veneer */
body::before {
  content: ''; position: fixed; inset: 0; pointer-events: none; z-index: 2;
  background-image: url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='180' height='180'><filter id='n'><feTurbulence type='fractalNoise' baseFrequency='0.85' numOctaves='2'/><feColorMatrix values='0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.22 0'/></filter><rect width='100%25' height='100%25' filter='url(%23n)'/></svg>");
  opacity: 0.55; mix-blend-mode: overlay;
}

/* page frame */
.page {
  max-width: 1180px; margin: 0 auto; padding: 0 32px;
  position: relative; z-index: 3;
}
.topbar, footer { position: relative; z-index: 3; }
@media (max-width: 720px) {
  .page { padding: 0 18px; }
  .sun { right: 6%; bottom: 22vh; width: 160px; height: 160px; }
}

/* ============ TOP BAR ============ */
.topbar {
  display: flex; align-items: center; justify-content: space-between;
  padding: 18px 32px;
  font-family: var(--mono); font-size: 12px;
  color: var(--ink-dim);
}
.topbar .brand { display: flex; align-items: center; gap: 10px; color: var(--ink); letter-spacing: 0.02em; }
.brand-mark {
  width: 14px; height: 14px; border-radius: 50%;
  background: radial-gradient(circle at 35% 35%, var(--sun) 0%, var(--amber) 55%, var(--sky-4) 100%);
  box-shadow: 0 0 12px rgba(244,200,120,0.55);
}
.topbar .nav { display: flex; gap: 22px; align-items: center; }
.topbar .nav a { color: var(--ink-dim); }
.topbar .nav a:hover { color: var(--ink); }
.live {
  display: inline-flex; align-items: center; gap: 6px;
}
.live .dot { width: 6px; height: 6px; border-radius: 50%; background: var(--green); box-shadow: 0 0 8px var(--green); animation: pulse 1.8s ease-in-out infinite; }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }

@media (max-width: 720px) {
  .topbar { padding: 14px 18px; }
  .topbar .nav { gap: 14px; }
  .topbar .nav .opt { display: none; }
}

/* ============ MASTHEAD ============ */
.mast {
  padding: 70px 0 60px;
  display: grid; grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr);
  gap: 56px; align-items: start;
}
.mast .eyebrow {
  font-family: var(--mono); font-size: 11px; letter-spacing: 0.22em; text-transform: uppercase;
  color: var(--amber); margin-bottom: 18px;
  display: flex; align-items: center; gap: 12px;
}
.mast .eyebrow::before { content: ''; width: 28px; height: 1px; background: var(--amber); display: inline-block; }
.mast h1 {
  font-family: var(--sans); font-size: 32px; font-weight: 600; letter-spacing: -0.018em; line-height: 1.18;
  margin-bottom: 20px; max-width: 22ch;
  color: var(--ink);
  text-shadow: 0 1px 22px rgba(15,8,22,0.45);
}
.mast h1 em {
  font-family: var(--serif); font-style: italic; font-weight: 500;
  color: var(--sun); letter-spacing: -0.008em;
}
.mast p {
  color: var(--ink-dim); font-size: 15.5px; line-height: 1.6; max-width: 58ch;
  text-shadow: 0 1px 12px rgba(15,8,22,0.4);
}
.mast p .strong { color: var(--ink); }

.mast .side {
  font-family: var(--mono); font-size: 12px; color: var(--ink-dim);
  background: var(--glass);
  border: 1px solid var(--glass-edge);
  border-radius: 10px;
  padding: 18px 20px;
  backdrop-filter: blur(14px) saturate(120%);
  -webkit-backdrop-filter: blur(14px) saturate(120%);
  box-shadow: 0 10px 36px rgba(10,4,18,0.32);
}
.mast .side dl {
  display: grid; grid-template-columns: 84px 1fr; gap: 8px 14px;
}
.mast .side dt { color: var(--ink-faint); text-transform: uppercase; letter-spacing: 0.12em; font-size: 10.5px; align-self: center; }
.mast .side dd { color: var(--ink); }
.mast .side dd.amber { color: var(--amber); }
.mast .side dd.green { color: var(--green); }

@media (max-width: 820px) {
  .mast { grid-template-columns: 1fr; gap: 28px; padding: 44px 0 32px; }
  .mast h1 { font-size: 26px; }
}

/* ============ SECTION ============ */
section.block {
  padding: 28px 0;
}
.panel {
  background: var(--glass);
  border: 1px solid var(--glass-edge);
  border-radius: 14px;
  padding: 26px 28px;
  backdrop-filter: blur(16px) saturate(120%);
  -webkit-backdrop-filter: blur(16px) saturate(120%);
  box-shadow: 0 18px 48px rgba(10,4,18,0.34);
}
@media (max-width: 640px) {
  .panel { padding: 20px 18px; border-radius: 12px; }
}
.section-head {
  display: flex; align-items: baseline; justify-content: space-between;
  margin-bottom: 22px; gap: 16px; flex-wrap: wrap;
}
.section-head h2 {
  font-size: 19px; font-weight: 600; letter-spacing: -0.005em;
}
.section-head h2 em { font-family: var(--serif); font-style: italic; font-weight: 500; color: var(--sun); }
.section-head .note {
  font-family: var(--mono); font-size: 11px; color: var(--ink-faint);
  text-transform: uppercase; letter-spacing: 0.16em;
}

/* ============ EXAMPLE ============ */
.example {
  display: grid; grid-template-columns: minmax(0, 1fr) 290px;
  gap: 20px; align-items: stretch;
}
@media (max-width: 860px) { .example { grid-template-columns: 1fr; } }

/* dark ink panel (used for code blocks — keeps code legible against painterly bg) */
.ink {
  background: rgba(10, 5, 18, 0.7);
  border: 1px solid var(--glass-edge);
  border-radius: 10px; overflow: hidden;
  backdrop-filter: blur(14px) saturate(110%);
  -webkit-backdrop-filter: blur(14px) saturate(110%);
}

.diff .pane { padding: 16px 18px; }
.diff .pane + .pane { border-top: 1px solid var(--glass-edge); }
.diff .pane-head {
  display: flex; align-items: center; justify-content: space-between;
  font-family: var(--mono); font-size: 11px; letter-spacing: 0.12em; text-transform: uppercase;
  color: var(--ink-faint); margin-bottom: 10px;
}
.diff .pane.before .pane-head .marker { color: var(--ink-faint); }
.diff .pane.after  .pane-head .marker { color: var(--red); }
.diff .pane-head .cost { color: var(--ink-dim); text-transform: none; letter-spacing: 0.04em; }
.diff .pane.after .pane-head .cost { color: var(--red); }
.diff pre {
  font-family: var(--mono); font-size: 12.5px; line-height: 1.7; color: var(--ink-dim);
  white-space: pre; overflow-x: auto;
}
.diff .k { color: #d29bd6; }
.diff .s { color: #e4c478; }
.diff .fn { color: var(--teal); }
.diff .cm { color: var(--ink-faint); font-style: italic; }
.diff .old { color: var(--ink-faint); text-decoration: line-through; text-decoration-color: var(--ink-faint); }
.diff .new { color: var(--red); background: rgba(224,112,96,0.16); padding: 0 4px; border-radius: 2px; }

.verdict {
  border-radius: 10px; padding: 20px;
  background: linear-gradient(160deg, rgba(244,200,120,0.18) 0%, rgba(20,10,28,0.55) 100%);
  border: 1px solid var(--glass-edge-strong);
  backdrop-filter: blur(14px) saturate(125%);
  -webkit-backdrop-filter: blur(14px) saturate(125%);
  display: flex; flex-direction: column; gap: 16px;
  box-shadow: 0 12px 32px rgba(10,4,18,0.4);
}
.verdict .big {
  font-family: var(--sans); font-size: 38px; font-weight: 700; color: var(--ink);
  letter-spacing: -0.02em; line-height: 1;
  text-shadow: 0 0 28px rgba(224,112,96,0.35);
}
.verdict .big .neg { color: var(--red); }
.verdict .big .unit { font-family: var(--serif); font-style: italic; font-weight: 400; font-size: 16px; color: var(--ink-dim); margin-left: 4px; }
.verdict dl {
  display: grid; grid-template-columns: 80px 1fr; gap: 8px 12px;
  font-family: var(--mono); font-size: 12px;
}
.verdict dt { color: var(--ink-faint); text-transform: uppercase; letter-spacing: 0.1em; font-size: 10.5px; align-self: center; }
.verdict dd { color: var(--ink); }
.verdict dd.red { color: var(--red); }
.verdict dd.amber { color: var(--amber); }
.verdict dd .accent { color: var(--amber); }

.formula {
  margin-top: 16px; border-radius: 10px;
  background: rgba(10,5,18,0.55);
  border: 1px solid var(--glass-edge);
  padding: 16px 18px;
  font-family: var(--mono); font-size: 12px; color: var(--ink-dim); line-height: 1.9;
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
}
.formula .row { display: grid; grid-template-columns: 80px 1fr auto; gap: 14px; align-items: baseline; }
.formula .row .lbl { color: var(--ink-faint); text-transform: uppercase; letter-spacing: 0.1em; font-size: 10.5px; }
.formula .row .res { color: var(--ink); text-align: right; }
.formula .row .res.red { color: var(--red); }
.formula .rule { height: 1px; background: var(--glass-edge); margin: 6px 0; }
@media (max-width: 540px) {
  .formula .row { grid-template-columns: 1fr; gap: 2px; }
  .formula .row .res { text-align: left; }
}

/* ============ METHOD ============ */
.method {
  display: grid; grid-template-columns: repeat(4, 1fr); gap: 18px;
}
@media (max-width: 820px) { .method { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 480px) { .method { grid-template-columns: 1fr; } }
.step {
  padding: 16px 18px;
  border-radius: 10px;
  background: rgba(20,12,28,0.4);
  border: 1px solid var(--glass-edge);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  transition: border-color 0.25s, transform 0.25s, background 0.25s;
}
.step:hover { border-color: var(--glass-edge-strong); transform: translateY(-2px); background: rgba(30,18,40,0.5); }
.step .n {
  font-family: var(--mono); font-size: 10.5px; letter-spacing: 0.16em; text-transform: uppercase;
  color: var(--amber); margin-bottom: 8px;
}
.step h3 { font-size: 14.5px; font-weight: 600; margin-bottom: 6px; color: var(--ink); }
.step p { font-size: 13px; color: var(--ink-dim); line-height: 1.55; }
.step .tool {
  margin-top: 12px; padding-top: 10px;
  border-top: 1px dashed var(--glass-edge);
  font-family: var(--mono); font-size: 10.5px;
  letter-spacing: 0.14em; text-transform: uppercase; color: var(--ink-faint);
}

/* ============ STATS ============ */
.stats {
  display: grid; grid-template-columns: repeat(4, 1fr); gap: 0;
  border-radius: 10px; overflow: hidden;
  border: 1px solid var(--glass-edge);
  background: rgba(20,12,28,0.42);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
}
.stats .cell { padding: 18px 20px; border-right: 1px solid var(--glass-edge); }
.stats .cell:last-child { border-right: 0; }
.stats .k { font-family: var(--mono); font-size: 10.5px; letter-spacing: 0.16em; text-transform: uppercase; color: var(--ink-faint); margin-bottom: 8px; }
.stats .v { font-family: var(--sans); font-size: 24px; font-weight: 700; color: var(--ink); letter-spacing: -0.015em; }
.stats .v.red { color: var(--red); }
.stats .v.amber { color: var(--amber); }
.stats .sub { font-size: 11.5px; color: var(--ink-faint); margin-top: 4px; font-family: var(--mono); }
@media (max-width: 820px) {
  .stats { grid-template-columns: repeat(2, 1fr); }
  .stats .cell:nth-child(2) { border-right: 0; }
  .stats .cell:nth-child(1), .stats .cell:nth-child(2) { border-bottom: 1px solid var(--glass-edge); }
}
@media (max-width: 480px) {
  .stats { grid-template-columns: 1fr; }
  .stats .cell { border-right: 0; border-bottom: 1px solid var(--glass-edge); }
  .stats .cell:last-child { border-bottom: 0; }
}

/* ============ REVIEWS ============ */
.reviews {
  border-radius: 10px; overflow: hidden;
  border: 1px solid var(--glass-edge);
  background: rgba(20,12,28,0.4);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
}
.review {
  display: grid; grid-template-columns: 76px minmax(0,1fr) 130px 70px;
  gap: 16px; align-items: center;
  padding: 13px 20px; border-bottom: 1px solid var(--glass-edge);
}
.review:last-child { border-bottom: 0; }
.review:hover { background: rgba(40,24,52,0.4); }
.review .title { color: var(--ink); font-size: 13.5px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.review .sub { font-family: var(--mono); font-size: 11px; color: var(--ink-faint); margin-top: 3px; }
.review .delta { font-family: var(--mono); font-size: 13.5px; font-weight: 600; text-align: right; }
.review .delta.up { color: var(--red); }
.review .delta.down { color: var(--green); }
.review .delta.flat { color: var(--ink-dim); }
.review .mr { font-family: var(--mono); font-size: 12px; text-align: right; color: var(--ink-dim); }
.review .mr:hover { color: var(--amber); }
@media (max-width: 720px) {
  .review { grid-template-columns: auto 1fr; }
  .review .delta, .review .mr { grid-column: 1 / -1; text-align: left; }
}

.tag {
  display: inline-flex; align-items: center; gap: 6px;
  font-family: var(--mono); font-size: 10.5px; letter-spacing: 0.12em; text-transform: uppercase;
  padding: 4px 9px; border-radius: 3px; border: 1px solid;
}
.tag .d { width: 5px; height: 5px; border-radius: 50%; }
.tag.block { color: var(--red); border-color: rgba(224,112,96,0.55); background: rgba(224,112,96,0.14); }
.tag.block .d { background: var(--red); box-shadow: 0 0 6px var(--red); }
.tag.warn  { color: var(--amber); border-color: rgba(243,173,90,0.55); background: rgba(243,173,90,0.14); }
.tag.warn .d { background: var(--amber); box-shadow: 0 0 6px var(--amber); }
.tag.pass  { color: var(--green); border-color: rgba(155,191,110,0.55); background: rgba(155,191,110,0.14); }
.tag.pass .d { background: var(--green); box-shadow: 0 0 6px var(--green); }
.tag.info  { color: var(--ink-dim); border-color: var(--glass-edge); background: rgba(20,12,28,0.4); }
.tag.info .d { background: var(--ink-dim); }

/* ============ SITES TABLE ============ */
.sites {
  border-radius: 10px; overflow: hidden;
  border: 1px solid var(--glass-edge);
  background: rgba(20,12,28,0.4);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
}
.sites table { width: 100%; border-collapse: collapse; font-size: 13px; }
.sites th {
  text-align: left; padding: 13px 20px;
  font-family: var(--mono); font-size: 10.5px; letter-spacing: 0.14em; text-transform: uppercase;
  color: var(--ink-faint); font-weight: 500;
  border-bottom: 1px solid var(--glass-edge);
  background: rgba(10,5,18,0.45);
}
.sites td {
  padding: 12px 20px; border-bottom: 1px solid var(--glass-edge); font-family: var(--mono); font-size: 12.5px;
  color: var(--ink-dim);
}
.sites tr:last-child td { border-bottom: 0; }
.sites tr:hover td { background: rgba(40,24,52,0.4); }
.sites .label { color: var(--ink); font-family: var(--sans); font-weight: 500; }
.sites .num { text-align: right; }
.sites .cost { text-align: right; color: var(--amber); font-weight: 600; position: relative; }
.sites .cost .bar {
  position: absolute; left: 0; right: 0; bottom: 0; height: 2px;
  background: linear-gradient(90deg, transparent, var(--amber) 70%);
  opacity: 0.75;
}

.empty {
  border: 1px dashed var(--glass-edge-strong); border-radius: 10px;
  padding: 28px 22px; text-align: center; color: var(--ink-faint); font-size: 13px;
  background: rgba(20,12,28,0.32);
}
.empty code { font-family: var(--mono); color: var(--amber); background: transparent; }
.empty p + p { margin-top: 6px; }

/* ============ FOOTER ============ */
footer {
  margin-top: 32px;
  padding: 24px 32px; display: flex; justify-content: space-between; align-items: center;
  font-family: var(--mono); font-size: 11.5px; color: var(--ink-faint);
  gap: 16px; flex-wrap: wrap;
  border-top: 1px solid var(--glass-edge);
  background: rgba(10,5,18,0.4);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
}
footer a { color: var(--ink-dim); }
footer a:hover { color: var(--amber); }
@media (max-width: 720px) { footer { padding: 18px; } }
</style>
</head>
<body>

<div class="terrain"></div>
<div class="sun"></div>

<header class="topbar">
  <div class="brand">
    <span class="brand-mark"></span>
    <span>spendlint</span>
    <span style="color: var(--ink-faint); margin-left: 4px;">v0.1</span>
  </div>
  <nav class="nav">
    <span class="live"><span class="dot"></span>live</span>
    <a href="#example" class="opt">example</a>
    <a href="#method" class="opt">method</a>
    <a href="#ledger">ledger</a>
    <a href="https://github.com/Yatsuiii/spendlint" target="_blank">source</a>
  </nav>
</header>

<div class="page">

  <section class="mast">
    <div>
      <div class="eyebrow">a linter for your LLM bill</div>
      <h1>spendlint reads every merge request and projects the <em>cost delta</em> before it ships.</h1>
      <p>Observability tells you after the bill arrives. <span class="strong">spendlint catches the change at code review time</span>, joins it to your team's real historical traffic from a labeled ledger, and posts a verdict on the MR. PASS, WARN, or BLOCK.</p>
    </div>
    <aside class="side">
      <dl>
        <dt>status</dt><dd class="green">Cloud Run &middot; live</dd>
        <dt>brain</dt><dd>Gemini 2.5 Pro</dd>
        <dt>cloud</dt><dd>Vertex AI</dd>
        <dt>partner</dt><dd>GitLab MCP</dd>
        <dt>license</dt><dd class="amber">MIT</dd>
        <dt>track</dt><dd>Rapid Agent / GitLab</dd>
      </dl>
    </aside>
  </section>

  <section class="block" id="example">
    <div class="panel">
      <div class="section-head">
        <h2>What it <em>catches</em></h2>
        <span class="note">example &middot; model swap</span>
      </div>
      <div class="example">
        <div>
          <div class="diff ink">
            <div class="pane before">
              <div class="pane-head">
                <span class="marker">&minus; before</span>
                <span class="cost">$0.2520 / day</span>
              </div>
<pre><span class="cm"># spendlint:label summary_endpoint</span>
<span class="k">def</span> <span class="fn">summarize</span>(text):
    resp = client.messages.create(
        model=<span class="old s">"claude-3-haiku"</span>,
        max_tokens=<span class="s">320</span>,
    )
    <span class="k">return</span> resp.content
</pre>
            </div>
            <div class="pane after">
              <div class="pane-head">
                <span class="marker">+ after</span>
                <span class="cost">$12.6600 / day</span>
              </div>
<pre><span class="cm"># spendlint:label summary_endpoint</span>
<span class="k">def</span> <span class="fn">summarize</span>(text):
    resp = client.messages.create(
        model=<span class="new s">"claude-3-5-sonnet"</span>,
        max_tokens=<span class="s">320</span>,
    )
    <span class="k">return</span> resp.content
</pre>
            </div>
          </div>
          <div class="formula">
            <div class="row"><span class="lbl">baseline</span><span>600 calls/day &times; (1,400 in &times; $0.25/M + 320 out &times; $1.25/M)</span><span class="res">$0.2520/day</span></div>
            <div class="row"><span class="lbl">projected</span><span>600 calls/day &times; (1,400 in &times; $3.00/M + 320 out &times; $15.00/M)</span><span class="res">$12.6600/day</span></div>
            <div class="rule"></div>
            <div class="row"><span class="lbl">delta</span><span>projected &minus; baseline</span><span class="res red">+$12.4080/day</span></div>
          </div>
        </div>

        <aside class="verdict">
          <div class="big"><span class="neg">+$12.41</span><span class="unit">/day</span></div>
          <dl>
            <dt>verdict</dt><dd><span class="tag block"><span class="d"></span>BLOCK</span></dd>
            <dt>change</dt><dd>model swap on <span class="accent">summary_endpoint</span></dd>
            <dt>rate</dt><dd class="red">&times;12 in &middot; &times;15 out</dd>
            <dt>monthly</dt><dd class="red">+$372.24</dd>
            <dt>confidence</dt><dd class="amber">high</dd>
          </dl>
        </aside>
      </div>
    </div>
  </section>

  <section class="block" id="method">
    <div class="panel">
      <div class="section-head">
        <h2>How it <em>works</em></h2>
        <span class="note">webhook &rarr; agent &rarr; verdict</span>
      </div>
      <div class="method">
        <div class="step">
          <div class="n">01 &middot; fetch</div>
          <h3>Pull the diff</h3>
          <p>GitLab webhook fires on every MR open or update. The agent pulls diff and metadata via MCP locally, REST in production.</p>
          <div class="tool">GitLab MCP / REST</div>
        </div>
        <div class="step">
          <div class="n">02 &middot; classify</div>
          <h3>Read the hunks</h3>
          <p>Gemini tags each change: model swap, retry loop added, max_tokens bumped, new or removed call site.</p>
          <div class="tool">Vertex Gemini 2.5</div>
        </div>
        <div class="step">
          <div class="n">03 &middot; project</div>
          <h3>Join to traffic</h3>
          <p>The novel core. Matches changed call sites to labeled history in the ledger, applies the pricing delta, returns a $/day projection.</p>
          <div class="tool">SQLite ledger</div>
        </div>
        <div class="step">
          <div class="n">04 &middot; comment</div>
          <h3>Post the verdict</h3>
          <p>Writes the verdict, the formula, and the assumptions back to the MR. Numbers are transparent. No magic.</p>
          <div class="tool">GitLab notes API</div>
        </div>
      </div>
    </div>
  </section>

  <section class="block" id="ledger">
    <div class="panel">
      <div class="section-head">
        <h2>Live <em>ledger</em></h2>
        <span class="note">this deployment &middot; real data</span>
      </div>

      <div class="stats" style="margin-bottom: 22px;">
        <div class="cell">
          <div class="k">MRs reviewed</div>
          <div class="v">{{.Summary.ReviewCount}}</div>
          <div class="sub">{{.Summary.BlockCount}} blocked &middot; {{.Summary.WarnCount}} warned</div>
        </div>
        <div class="cell">
          <div class="k">Flagged spend</div>
          <div class="v red">${{printf "%.4f" .Summary.FlaggedPerDay}}</div>
          <div class="sub">$/day caught pre-merge</div>
        </div>
        <div class="cell">
          <div class="k">Saved spend</div>
          <div class="v">${{printf "%.4f" .Summary.SavedPerDay}}</div>
          <div class="sub">$/day from reductions</div>
        </div>
        <div class="cell">
          <div class="k">Ledger volume</div>
          <div class="v amber">${{printf "%.4f" .Summary.TotalLedgerPerDay}}</div>
          <div class="sub">$/day observed</div>
        </div>
      </div>

      <div class="section-head" style="margin-top: 22px;">
        <h2 style="font-size: 14px; color: var(--ink-dim); font-weight: 500;">Recent reviews</h2>
        <span class="note">latest 20</span>
      </div>
      {{if .Reviews}}
      <div class="reviews">
        {{range .Reviews}}
        <div class="review">
          <span class="tag {{verdictClass .Verdict}}"><span class="d"></span>{{.Verdict}}</span>
          <div>
            <div class="title">{{.MRTitle}}</div>
            <div class="sub">{{.Project}} &middot; {{.Timestamp.Format "Jan 2, 15:04"}}</div>
          </div>
          <div class="delta {{deltaSign .DeltaDay}}">{{fmtDelta .DeltaDay}}/day</div>
          <a class="mr" href="https://gitlab.com/{{.Project}}/-/merge_requests/{{.MRIID}}" target="_blank">!{{.MRIID}} &rarr;</a>
        </div>
        {{end}}
      </div>
      {{else}}
      <div class="empty">
        <p>No reviews yet on this instance.</p>
        <p>Point a GitLab webhook at <code>/webhook</code> on this URL and merge requests start landing here.</p>
      </div>
      {{end}}

      <div class="section-head" style="margin-top: 28px;">
        <h2 style="font-size: 14px; color: var(--ink-dim); font-weight: 500;">Call-site traffic</h2>
        <span class="note">labeled in source</span>
      </div>
      {{if .Stats}}
      <div class="sites">
        <table>
          <thead>
            <tr>
              <th>Label</th>
              <th>Model</th>
              <th class="num">Calls/day</th>
              <th class="num">Avg in</th>
              <th class="num">Avg out</th>
              <th class="num">$/day</th>
            </tr>
          </thead>
          <tbody>
            {{$max := .MaxCostPerDay}}
            {{range .Stats}}
            <tr>
              <td class="label">{{.Label}}</td>
              <td>{{.DominantModel}}</td>
              <td class="num">{{printf "%.1f" .CallsPerDay}}</td>
              <td class="num">{{printf "%.0f" .AvgInTokens}}</td>
              <td class="num">{{printf "%.0f" .AvgOutTokens}}</td>
              <td class="num cost">${{printf "%.4f" .CostPerDayUSD}}<div class="bar" style="width: {{barWidth .CostPerDayUSD $max}};"></div></td>
            </tr>
            {{end}}
          </tbody>
        </table>
      </div>
      {{else}}
      <div class="empty">
        <p>No calls recorded yet.</p>
        <p>Wrap LLM calls with <code>recorder.Record(label, model, prompt, in, out)</code> and the ledger fills automatically.</p>
      </div>
      {{end}}
    </div>
  </section>

</div>

<footer>
  <div>spendlint &middot; <a href="https://github.com/Yatsuiii/spendlint" target="_blank">MIT</a> &middot; built for the Google Cloud Rapid Agent hackathon</div>
  <div>GitLab Track &middot; <a href="https://github.com/Yatsuiii/spendlint" target="_blank">Yatsuiii</a></div>
</footer>

</body>
</html>
`

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

	summary := computeDashStats(reviews, stats)
	var maxCost float64
	for _, st := range stats {
		if st.CostPerDayUSD > maxCost {
			maxCost = st.CostPerDayUSD
		}
	}

	data := struct {
		Reviews       []ledger.Review
		Stats         []ledger.SiteStats
		Summary       dashStats
		MaxCostPerDay float64
		Now           time.Time
	}{reviews, stats, summary, maxCost, time.Now()}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = dashTmpl.Execute(w, data)
}
