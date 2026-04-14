// B2 — Safer heuristic (design/14 Phase B)
//
// drover-code estimates tokens as runes/charsPerToken (default 4). That is not
// the API tokenizer; it is a cheap, stable signal for compaction timing.
//
// Guidance (corpus-free, operator-facing):
//
//   - English prose and typical ASCII code often land near ~3.5–4.5 characters
//     per token for Claude-family models, but JSON-heavy tool chatter, CJK, or
//     highly symbolic code can deviate.
//
//   - If /tokens "API calibration" EMA stays above ~1.05–1.1, the service counts
//     more input tokens than our heuristic for the same prompt shape: lower
//     charsPerToken (e.g. 4->3) or lower contextLimitEstimate so compaction runs
//     earlier (safer against 400s, more compaction churn).
//
//   - If EMA stays below ~0.9, the heuristic is pessimistic: you may raise
//     charsPerToken slightly to reduce premature compaction.
//
// Prefer tuning with live API calibration (B3) over guessing; use this table
// only to reason about divisor direction.

package convo

import (
	"strings"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/api"
)

func TestB2_divisorEffect_onSyntheticCorpus(t *testing.T) {
	// Synthetic "corpus": ASCII-like lines (not real tokenizer ground truth).
	corpus := strings.Repeat("func foo() { return x + 1 }\n", 200)
	sys := strings.Repeat("# ", 50)
	m := NewManagerWithSystem(sys)
	m.Append(api.UserMessage(corpus))

	m.SetCharsPerToken(4)
	at4 := m.EstimatedTokens()
	m.SetCharsPerToken(3)
	at3 := m.EstimatedTokens()
	m.SetCharsPerToken(5)
	at5 := m.EstimatedTokens()

	if at3 <= at4 || at4 <= at5 {
		t.Fatalf("expected at3 > at4 > at5, got %d %d %d", at3, at4, at5)
	}
}
