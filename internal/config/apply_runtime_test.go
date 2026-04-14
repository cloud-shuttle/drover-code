package config

import (
	"testing"

	"github.com/cloudshuttle/drover-code/internal/convo"
)

func TestApplyConvoHeuristics_contextAndChars(t *testing.T) {
	m := convo.NewManagerWithSystem("sys")
	t.Setenv("DROVER_CODE_CONTEXT_LIMIT_EST", "")
	t.Setenv("DROVER_CODE_CHARS_PER_TOKEN", "")

	ApplyConvoHeuristics(m, Settings{ContextLimitEstimate: 99_000})
	if m.ContextLimit() != 99_000 {
		t.Fatalf("context limit: %d", m.ContextLimit())
	}

	t.Setenv("DROVER_CODE_CONTEXT_LIMIT_EST", "120000")
	ApplyConvoHeuristics(m, Settings{ContextLimitEstimate: 99_000})
	if m.ContextLimit() != 120_000 {
		t.Fatalf("env override: %d", m.ContextLimit())
	}

	m2 := convo.NewManagerWithSystem("x")
	ApplyConvoHeuristics(m2, Settings{CharsPerTokenEstimate: 3})
	if m2.CharsPerToken() != 3 {
		t.Fatalf("chars: %d", m2.CharsPerToken())
	}
	t.Setenv("DROVER_CODE_CHARS_PER_TOKEN", "5")
	ApplyConvoHeuristics(m2, Settings{CharsPerTokenEstimate: 3})
	if m2.CharsPerToken() != 5 {
		t.Fatalf("env chars: %d", m2.CharsPerToken())
	}
}
