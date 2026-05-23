// Package recorder is the thin instrumentation a team embeds at each LLM call
// site. It tags the call with a stable label, computes cost from the pricing
// table, and appends a row to the ledger. That labeled row is what the cost
// projector later joins a code change against.
package recorder

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/Yatsuiii/spendlint/internal/ledger"
	"github.com/Yatsuiii/spendlint/internal/pricing"
)

// Recorder writes recorded calls to a ledger.
type Recorder struct {
	led *ledger.Ledger
	now func() time.Time
}

// New returns a Recorder backed by led.
func New(led *ledger.Ledger) *Recorder {
	return &Recorder{led: led, now: time.Now}
}

// Record logs one completed call. Cost is derived from the model's pricing;
// an unknown model records a zero cost (the provider is still captured). The
// stored call is returned with its ledger ID set.
func (r *Recorder) Record(label, model, prompt string, inTok, outTok int) (ledger.Call, error) {
	rate, known := pricing.Lookup(model)
	provider := ""
	cost := 0.0
	if known {
		provider = rate.Provider
		cost = pricing.CostFor(rate, inTok, outTok)
	}
	c := ledger.Call{
		Timestamp:    r.now(),
		Label:        label,
		Provider:     provider,
		Model:        model,
		InputTokens:  inTok,
		OutputTokens: outTok,
		CostUSD:      cost,
		PromptHash:   PromptHash(prompt),
	}
	if err := r.led.Record(&c); err != nil {
		return ledger.Call{}, err
	}
	return c, nil
}

// PromptHash returns a short stable digest of a prompt. The ledger stores the
// hash, never the prompt text, so recorded traffic carries no payload.
func PromptHash(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:8])
}
