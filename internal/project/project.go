// Package project is the cost-projection engine: the novel core of spendlint.
// It takes a slice of classified changes from a diff, looks up historical
// traffic from the ledger, and computes the projected $/day delta.
//
// Formula (per call site):
//
//	baseline  = calls_per_day * (avg_in * in_rate_old + avg_out * out_rate_old) / 1e6
//	projected = calls_per_day * vol_mult * (avg_in * in_rate_new + avg_out_new * out_rate_new) / 1e6
//	delta     = projected - baseline
package project

import (
	"fmt"
	"strconv"
	"strings"

	diffpkg "github.com/Yatsuiii/spendlint/internal/diff"
	"github.com/Yatsuiii/spendlint/internal/ledger"
	"github.com/Yatsuiii/spendlint/internal/pricing"
)

// Result is the projection for one classified change.
type Result struct {
	Label           string
	ChangeType      diffpkg.ChangeType
	OldValue        string
	NewValue        string
	BaselineDayUSD  float64
	ProjectedDayUSD float64
	DeltaDayUSD     float64 // positive = cost increase
	Confidence      string  // high | medium | low
	Assumption      string  // human-readable caveat
}

// Projector computes cost deltas from diff changes + ledger stats.
type Projector struct {
	led *ledger.Ledger
}

// New returns a Projector backed by the given ledger.
func New(led *ledger.Ledger) *Projector { return &Projector{led: led} }

// Project returns one Result per change that could affect cost. Changes with
// no recognizable label or no ledger history are still returned with a low
// confidence note; the caller decides whether to surface them.
func (p *Projector) Project(changes []diffpkg.Change) ([]Result, error) {
	var results []Result
	for _, c := range changes {
		r, err := p.projectOne(c)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

func (p *Projector) projectOne(c diffpkg.Change) (Result, error) {
	r := Result{
		Label:      c.Label,
		ChangeType: c.Type,
		OldValue:   c.OldValue,
		NewValue:   c.NewValue,
	}

	stats, hasStats, err := p.siteStats(c.Label)
	if err != nil {
		return r, err
	}

	switch c.Type {

	case diffpkg.ChangeModelSwap:
		oldRate, oldOK := pricing.Lookup(c.OldValue)
		newRate, newOK := pricing.Lookup(c.NewValue)
		if !oldOK || !newOK {
			r.Confidence = "low"
			r.Assumption = fmt.Sprintf("model %q or %q not in pricing table", c.OldValue, c.NewValue)
			return r, nil
		}
		if !hasStats {
			r.Confidence = "low"
			r.Assumption = fmt.Sprintf("no recorded traffic for label %q; cannot project volume", labelOrFile(c))
			// Still compute a unit-cost delta so the comment is informative.
			r.BaselineDayUSD = pricing.CostFor(oldRate, 1000, 200)
			r.ProjectedDayUSD = pricing.CostFor(newRate, 1000, 200)
			r.DeltaDayUSD = r.ProjectedDayUSD - r.BaselineDayUSD
			r.Assumption += fmt.Sprintf("; showing unit cost (1k in / 200 out): %s -> %s",
				formatCostM(oldRate), formatCostM(newRate))
			return r, nil
		}
		inTok := stats.AvgInTokens
		outTok := stats.AvgOutTokens
		r.BaselineDayUSD = stats.CallsPerDay * pricing.CostFor(oldRate, int(inTok), int(outTok))
		r.ProjectedDayUSD = stats.CallsPerDay * pricing.CostFor(newRate, int(inTok), int(outTok))
		r.DeltaDayUSD = r.ProjectedDayUSD - r.BaselineDayUSD
		r.Confidence = "high"
		r.Assumption = fmt.Sprintf("%.1f calls/day, avg %d in / %d out tokens (30-day ledger average)",
			stats.CallsPerDay, int(inTok), int(outTok))

	case diffpkg.ChangeMaxTokens:
		oldTok, _ := strconv.Atoi(c.OldValue)
		newTok, _ := strconv.Atoi(c.NewValue)
		if !hasStats {
			r.Confidence = "low"
			r.Assumption = fmt.Sprintf("no recorded traffic for label %q", labelOrFile(c))
			return r, nil
		}
		rate, ok := pricing.Lookup(stats.DominantModel)
		if !ok {
			r.Confidence = "low"
			r.Assumption = fmt.Sprintf("model %q not in pricing table", stats.DominantModel)
			return r, nil
		}
		r.BaselineDayUSD = stats.CallsPerDay * pricing.CostFor(rate, int(stats.AvgInTokens), oldTok)
		r.ProjectedDayUSD = stats.CallsPerDay * pricing.CostFor(rate, int(stats.AvgInTokens), newTok)
		r.DeltaDayUSD = r.ProjectedDayUSD - r.BaselineDayUSD
		r.Confidence = "medium"
		r.Assumption = fmt.Sprintf("output tokens capped at new max; actual output may be lower")

	case diffpkg.ChangeVolumeAdded:
		if !hasStats {
			r.Confidence = "low"
			r.Assumption = "no recorded traffic; cannot estimate volume multiplier"
			return r, nil
		}
		// Conservative: assume 3x multiplier for a retry/loop wrapper.
		const mult = 3.0
		rate, ok := pricing.Lookup(stats.DominantModel)
		if !ok {
			r.Confidence = "low"
			return r, nil
		}
		base := stats.CallsPerDay * pricing.CostFor(rate, int(stats.AvgInTokens), int(stats.AvgOutTokens))
		r.BaselineDayUSD = base
		r.ProjectedDayUSD = base * mult
		r.DeltaDayUSD = r.ProjectedDayUSD - r.BaselineDayUSD
		r.Confidence = "medium"
		r.Assumption = fmt.Sprintf("assumed %.0fx volume multiplier for added retry/loop wrapper", mult)

	case diffpkg.ChangeVolumeRemoved:
		if !hasStats {
			r.Confidence = "low"
			return r, nil
		}
		const mult = 3.0
		rate, ok := pricing.Lookup(stats.DominantModel)
		if !ok {
			r.Confidence = "low"
			return r, nil
		}
		base := stats.CallsPerDay * pricing.CostFor(rate, int(stats.AvgInTokens), int(stats.AvgOutTokens))
		r.BaselineDayUSD = base * mult
		r.ProjectedDayUSD = base
		r.DeltaDayUSD = r.ProjectedDayUSD - r.BaselineDayUSD
		r.Confidence = "medium"
		r.Assumption = fmt.Sprintf("assumed %.0fx volume reduction from removing retry/loop wrapper", mult)

	case diffpkg.ChangeCallAdded:
		r.Confidence = "low"
		r.Assumption = "new call site; no historical baseline. Cost depends on traffic to this endpoint."

	case diffpkg.ChangeCallRemoved:
		if hasStats {
			rate, ok := pricing.Lookup(stats.DominantModel)
			if ok {
				r.BaselineDayUSD = stats.CallsPerDay * pricing.CostFor(rate, int(stats.AvgInTokens), int(stats.AvgOutTokens))
				r.DeltaDayUSD = -r.BaselineDayUSD
				r.Confidence = "high"
				r.Assumption = fmt.Sprintf("removes %.1f calls/day at current traffic", stats.CallsPerDay)
				return r, nil
			}
		}
		r.Confidence = "low"
	}

	return r, nil
}

// siteStats fetches per-label stats from the ledger. If label is empty, it
// returns false without an error (unlabeled changes have no join key).
func (p *Projector) siteStats(label string) (ledger.SiteStats, bool, error) {
	if strings.TrimSpace(label) == "" {
		return ledger.SiteStats{}, false, nil
	}
	return p.led.StatsForLabel(label)
}

func labelOrFile(c diffpkg.Change) string {
	if c.Label != "" {
		return c.Label
	}
	return c.File
}

func formatCostM(r pricing.Rate) string {
	return fmt.Sprintf("$%.3f/$%.3f per 1M tokens", r.InputPerM, r.OutputPerM)
}

// TotalDelta sums the DeltaDayUSD across all results.
func TotalDelta(results []Result) float64 {
	var sum float64
	for _, r := range results {
		sum += r.DeltaDayUSD
	}
	return sum
}

// Verdict returns a short decision string based on the total delta.
func Verdict(totalDeltaDay float64) string {
	switch {
	case totalDeltaDay > 10:
		return "BLOCK - projected cost increase exceeds $10/day"
	case totalDeltaDay > 1:
		return "WARN - projected cost increase exceeds $1/day"
	case totalDeltaDay < -1:
		return "INFO - projected cost saving"
	default:
		return "PASS - cost impact within threshold"
	}
}
