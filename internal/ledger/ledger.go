// Package ledger is the SQLite store of recorded LLM calls and the per-label
// rollups the cost projector joins against. Each call carries a stable
// call-site label; that label is the bridge from a changed line of code to
// historical traffic.
package ledger

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Call is one recorded LLM API call.
type Call struct {
	ID           int64
	Timestamp    time.Time
	Label        string // stable call-site identifier (the join key)
	Provider     string // anthropic | openai | google
	Model        string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	PromptHash   string
}

// SiteStats is the per-label rollup the projector uses to turn a diff change
// into a dollar delta.
type SiteStats struct {
	Label         string
	Calls         int64
	CallsPerDay   float64
	AvgInTokens   float64
	AvgOutTokens  float64
	DominantModel string
	TotalCostUSD  float64
	CostPerDayUSD float64
}

// Ledger wraps the SQLite database handle.
type Ledger struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS reviews (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp    TEXT    NOT NULL,
	project      TEXT    NOT NULL,
	mr_iid       INTEGER NOT NULL,
	mr_title     TEXT    NOT NULL DEFAULT '',
	verdict      TEXT    NOT NULL,
	delta_day    REAL    NOT NULL,
	comment_body TEXT    NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS calls (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp     TEXT    NOT NULL,
	label         TEXT    NOT NULL,
	provider      TEXT    NOT NULL,
	model         TEXT    NOT NULL,
	input_tokens  INTEGER NOT NULL,
	output_tokens INTEGER NOT NULL,
	cost_usd      REAL    NOT NULL,
	prompt_hash   TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_calls_label ON calls(label);
`

// Open opens (creating if needed) the ledger at path and ensures the schema.
func Open(path string) (*Ledger, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open ledger %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Ledger{db: db}, nil
}

// Close closes the underlying database.
func (l *Ledger) Close() error { return l.db.Close() }

// DB exposes the underlying handle for callers that need a raw transaction.
func (l *Ledger) DB() *sql.DB { return l.db }

// Record inserts one call and sets its ID.
func (l *Ledger) Record(c *Call) error {
	res, err := l.db.Exec(
		`INSERT INTO calls (timestamp, label, provider, model, input_tokens, output_tokens, cost_usd, prompt_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Timestamp.UTC().Format(time.RFC3339Nano), c.Label, c.Provider, c.Model,
		c.InputTokens, c.OutputTokens, c.CostUSD, c.PromptHash,
	)
	if err != nil {
		return fmt.Errorf("insert call: %w", err)
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

// Clear removes all calls.
func (l *Ledger) Clear() error {
	if _, err := l.db.Exec(`DELETE FROM calls`); err != nil {
		return fmt.Errorf("clear calls: %w", err)
	}
	return nil
}

// windowDays returns the observation window in days, from the earliest to the
// latest recorded call, with a floor of 1. All per-day rates share this
// denominator so that volumes are comparable across labels.
func (l *Ledger) windowDays() (float64, error) {
	var minTS, maxTS sql.NullString
	row := l.db.QueryRow(`SELECT MIN(timestamp), MAX(timestamp) FROM calls`)
	if err := row.Scan(&minTS, &maxTS); err != nil {
		return 0, fmt.Errorf("scan window: %w", err)
	}
	if !minTS.Valid || !maxTS.Valid {
		return 1, nil
	}
	lo, err := time.Parse(time.RFC3339Nano, minTS.String)
	if err != nil {
		return 0, fmt.Errorf("parse min timestamp: %w", err)
	}
	hi, err := time.Parse(time.RFC3339Nano, maxTS.String)
	if err != nil {
		return 0, fmt.Errorf("parse max timestamp: %w", err)
	}
	days := hi.Sub(lo).Hours() / 24
	if days < 1 {
		return 1, nil
	}
	return days, nil
}

// Stats returns the per-label rollup, ordered by total cost descending so the
// most expensive call sites surface first.
func (l *Ledger) Stats() ([]SiteStats, error) {
	days, err := l.windowDays()
	if err != nil {
		return nil, err
	}
	rows, err := l.db.Query(`
		SELECT label,
		       COUNT(*)            AS calls,
		       AVG(input_tokens)   AS avg_in,
		       AVG(output_tokens)  AS avg_out,
		       SUM(cost_usd)       AS total_cost
		FROM calls
		GROUP BY label
		ORDER BY total_cost DESC`)
	if err != nil {
		return nil, fmt.Errorf("query stats: %w", err)
	}
	defer rows.Close()

	var out []SiteStats
	for rows.Next() {
		var s SiteStats
		if err := rows.Scan(&s.Label, &s.Calls, &s.AvgInTokens, &s.AvgOutTokens, &s.TotalCostUSD); err != nil {
			return nil, fmt.Errorf("scan stats: %w", err)
		}
		s.CallsPerDay = float64(s.Calls) / days
		s.CostPerDayUSD = s.TotalCostUSD / days
		model, err := l.dominantModel(s.Label)
		if err != nil {
			return nil, err
		}
		s.DominantModel = model
		out = append(out, s)
	}
	return out, rows.Err()
}

// dominantModel returns the most-used model for a label.
func (l *Ledger) dominantModel(label string) (string, error) {
	var model string
	row := l.db.QueryRow(`
		SELECT model FROM calls
		WHERE label = ?
		GROUP BY model
		ORDER BY COUNT(*) DESC
		LIMIT 1`, label)
	if err := row.Scan(&model); err != nil {
		return "", fmt.Errorf("dominant model for %s: %w", label, err)
	}
	return model, nil
}

// Review is one recorded MR review.
type Review struct {
	ID          int64
	Timestamp   time.Time
	Project     string
	MRIID       int
	MRTitle     string
	Verdict     string
	DeltaDay    float64
	CommentBody string
}

// RecordReview inserts one review record.
func (l *Ledger) RecordReview(r *Review) error {
	res, err := l.db.Exec(
		`INSERT INTO reviews (timestamp, project, mr_iid, mr_title, verdict, delta_day, comment_body)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.Timestamp.UTC().Format(time.RFC3339), r.Project, r.MRIID, r.MRTitle, r.Verdict, r.DeltaDay, r.CommentBody,
	)
	if err != nil {
		return fmt.Errorf("insert review: %w", err)
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

// RecentReviews returns up to n reviews, newest first.
func (l *Ledger) RecentReviews(n int) ([]Review, error) {
	rows, err := l.db.Query(
		`SELECT id, timestamp, project, mr_iid, mr_title, verdict, delta_day, comment_body
		 FROM reviews ORDER BY id DESC LIMIT ?`, n)
	if err != nil {
		return nil, fmt.Errorf("query reviews: %w", err)
	}
	defer rows.Close()
	var out []Review
	for rows.Next() {
		var rv Review
		var ts string
		if err := rows.Scan(&rv.ID, &ts, &rv.Project, &rv.MRIID, &rv.MRTitle, &rv.Verdict, &rv.DeltaDay, &rv.CommentBody); err != nil {
			return nil, fmt.Errorf("scan review: %w", err)
		}
		rv.Timestamp, _ = time.Parse(time.RFC3339, ts)
		out = append(out, rv)
	}
	return out, rows.Err()
}

// StatsForLabel returns the rollup for a single label, or false if the label
// has no recorded calls.
func (l *Ledger) StatsForLabel(label string) (SiteStats, bool, error) {
	all, err := l.Stats()
	if err != nil {
		return SiteStats{}, false, err
	}
	for _, s := range all {
		if s.Label == label {
			return s, true, nil
		}
	}
	return SiteStats{}, false, nil
}
