package project

import (
	"path/filepath"
	"testing"
	diffpkg "github.com/Yatsuiii/spendlint/internal/diff"
	"github.com/Yatsuiii/spendlint/internal/ledger"
	"github.com/Yatsuiii/spendlint/internal/recorder"
)

// populatedLedger inserts 30 days of synthetic calls for summary_endpoint
// (600 calls/day, claude-3-haiku, ~1400 in / ~320 out tokens).
func populatedLedger(t *testing.T) *ledger.Ledger {
	t.Helper()
	led, err := ledger.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { led.Close() })
	rec := recorder.New(led)
	for i := 0; i < 18000; i++ { // 600/day * 30 days
		if _, err := rec.Record("summary_endpoint", "claude-3-haiku-20240307", "prompt", 1400, 320); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	return led
}

func TestGoldPathModelSwap(t *testing.T) {
	led := populatedLedger(t)
	proj := New(led)

	changes := []diffpkg.Change{{
		Hunk:     diffpkg.Hunk{File: "summarize.py"},
		Type:     diffpkg.ChangeModelSwap,
		Label:    "summary_endpoint",
		OldValue: "claude-3-haiku",
		NewValue: "claude-3-5-sonnet",
	}}
	results, err := proj.Project(changes)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Confidence != "high" {
		t.Errorf("confidence = %q, want high", r.Confidence)
	}
	// delta must be positive (sonnet > haiku)
	if r.DeltaDayUSD <= 0 {
		t.Errorf("delta = %.4f $/day, want positive", r.DeltaDayUSD)
	}
	if TotalDelta(results) < 1 {
		t.Error("total delta should trigger WARN/BLOCK")
	}
}

func TestProjectNoLabel(t *testing.T) {
	led := populatedLedger(t)
	proj := New(led)
	changes := []diffpkg.Change{{
		Type:     diffpkg.ChangeModelSwap,
		OldValue: "claude-3-haiku",
		NewValue: "claude-3-5-sonnet",
	}}
	results, err := proj.Project(changes)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if results[0].Confidence != "low" {
		t.Error("unlabeled swap should be low confidence")
	}
}

func TestVerdictThresholds(t *testing.T) {
	cases := []struct {
		delta float64
		want  string
	}{
		{15, "BLOCK"},
		{2, "WARN"},
		{0.1, "PASS"},
		{-2, "INFO"},
	}
	for _, tc := range cases {
		v := Verdict(tc.delta)
		if v[:len(tc.want)] != tc.want {
			t.Errorf("Verdict(%.1f) = %q, want prefix %q", tc.delta, v, tc.want)
		}
	}
}

func TestProjectUnknownLabel(t *testing.T) {
	led, _ := ledger.Open(filepath.Join(t.TempDir(), "empty.db"))
	defer led.Close()
	proj := New(led)
	results, err := proj.Project([]diffpkg.Change{{
		Type: diffpkg.ChangeModelSwap, Label: "nonexistent",
		OldValue: "claude-3-haiku", NewValue: "claude-3-5-sonnet",
	}})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if results[0].Confidence != "low" {
		t.Errorf("expected low confidence for missing label, got %q", results[0].Confidence)
	}
}

