package ledger

import (
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Ledger {
	t.Helper()
	led, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { led.Close() })
	return led
}

func TestRecordAndStats(t *testing.T) {
	led := open(t)
	base := time.Now().UTC().Add(-10 * 24 * time.Hour)
	// 10 calls for "a" spread over a 10-day window, plus 1 for "b".
	for i := 0; i < 10; i++ {
		c := Call{
			Timestamp: base.Add(time.Duration(i) * 24 * time.Hour),
			Label:     "a", Provider: "anthropic", Model: "claude-3-haiku",
			InputTokens: 100, OutputTokens: 50, CostUSD: 1.0, PromptHash: "x",
		}
		if err := led.Record(&c); err != nil {
			t.Fatalf("Record: %v", err)
		}
		if c.ID == 0 {
			t.Error("Record did not set ID")
		}
	}
	if err := led.Record(&Call{Timestamp: base, Label: "b", Model: "gpt-4o", CostUSD: 5.0}); err != nil {
		t.Fatalf("Record b: %v", err)
	}

	stats, err := led.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	// Ordered by total cost desc: a totals $10, b totals $5.
	if len(stats) != 2 || stats[0].Label != "a" {
		t.Fatalf("stats = %+v, want a first", stats)
	}
	a := stats[0]
	if a.Calls != 10 || a.TotalCostUSD != 10 {
		t.Errorf("a calls=%d total=%v, want 10/10", a.Calls, a.TotalCostUSD)
	}
	if a.AvgInTokens != 100 || a.AvgOutTokens != 50 {
		t.Errorf("a avg in/out = %v/%v, want 100/50", a.AvgInTokens, a.AvgOutTokens)
	}
	if a.DominantModel != "claude-3-haiku" {
		t.Errorf("a dominant = %q", a.DominantModel)
	}
	// Window is 9 days (day 0 to day 9): 10 calls / 9 days.
	if a.CallsPerDay < 1.05 || a.CallsPerDay > 1.2 {
		t.Errorf("a calls/day = %.3f, want ~1.11", a.CallsPerDay)
	}
}

func TestClearMakesSeedIdempotent(t *testing.T) {
	led := open(t)
	led.Record(&Call{Timestamp: time.Now(), Label: "a", CostUSD: 1})
	if err := led.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	stats, err := led.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("after Clear stats = %+v, want empty", stats)
	}
}
