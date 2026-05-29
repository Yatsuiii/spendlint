// Package pricing holds per-model token rates and computes per-call cost.
// Rates are list prices in USD per 1,000,000 tokens, the unit every major
// provider publishes in. The projector reads these rates to turn a model
// swap into a dollar delta.
package pricing

import "strings"

// Rate is a model's list price, USD per 1,000,000 tokens.
type Rate struct {
	Provider   string
	InputPerM  float64
	OutputPerM float64
}

// table maps a canonical model id to its rate. Values are public list prices
// as of 2026-05; the gold-path haiku -> sonnet swap is ~12x per token, which
// these rows preserve.
var table = map[string]Rate{
	// Anthropic
	"claude-3-haiku":    {"anthropic", 0.25, 1.25},
	"claude-3-5-haiku":  {"anthropic", 0.80, 4.00},
	"claude-3-5-sonnet": {"anthropic", 3.00, 15.00},
	"claude-3-7-sonnet": {"anthropic", 3.00, 15.00},
	"claude-3-opus":     {"anthropic", 15.00, 75.00},
	// OpenAI
	"gpt-4o":      {"openai", 2.50, 10.00},
	"gpt-4o-mini": {"openai", 0.15, 0.60},
	// Google
	"gemini-1.5-flash":      {"google", 0.075, 0.30},
	"gemini-1.5-pro":        {"google", 1.25, 5.00},
	"gemini-2.5-flash-lite": {"google", 0.10, 0.40},
	"gemini-2.5-flash":      {"google", 0.30, 2.50},
	"gemini-2.5-pro":        {"google", 1.25, 10.00},
	"gemini-3.5-flash":      {"google", 0.075, 0.30},
}

// Lookup returns the rate for a model id. Matching is case-insensitive and
// tolerates a provider-style prefix (e.g. "anthropic/claude-3-haiku") and a
// trailing date suffix (e.g. "claude-3-5-sonnet-20241022").
func Lookup(model string) (Rate, bool) {
	key := normalize(model)
	if r, ok := table[key]; ok {
		return r, true
	}
	for id, r := range table {
		if strings.HasPrefix(key, id) {
			return r, true
		}
	}
	return Rate{}, false
}

func normalize(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	return m
}

// Cost returns the USD cost of a single call given input and output token
// counts. The bool is false when the model is unknown, in which case cost is 0.
func Cost(model string, inTok, outTok int) (float64, bool) {
	r, ok := Lookup(model)
	if !ok {
		return 0, false
	}
	return CostFor(r, inTok, outTok), true
}

// CostFor computes USD cost for a known rate and token counts.
func CostFor(r Rate, inTok, outTok int) float64 {
	return float64(inTok)/1e6*r.InputPerM + float64(outTok)/1e6*r.OutputPerM
}

// Models returns the known model ids, for diagnostics and the pricing view.
func Models() []string {
	out := make([]string, 0, len(table))
	for id := range table {
		out = append(out, id)
	}
	return out
}
