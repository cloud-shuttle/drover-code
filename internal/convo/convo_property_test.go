package convo

import (
	"fmt"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/api"
	"pgregory.net/rapid"
)

func TestProperty_EstimatedTokensNonNegative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sys := rapid.String().Draw(t, "sys")
		body := rapid.String().Draw(t, "body")

		m := NewManagerWithSystem(sys)
		m.Append(api.UserMessage(body))
		m.Append(api.AssistantMessage([]api.ContentBlock{
			api.TextBlock{Text: body},
			api.ToolUseBlock{ID: "x", Name: "n", Input: []byte(`{}`)},
		}))
		m.Append(api.ToolResultMessage([]api.ToolResultBlock{
			{ToolUseID: "x", Content: body},
		}))

		if m.EstimatedTokens() < 0 {
			t.Fatalf("EstimatedTokens should be non-negative, got: %d", m.EstimatedTokens())
		}
	})
}

func TestProperty_SummariseMessageCount(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		count := rapid.IntRange(1, 30).Draw(t, "count")
		keep := rapid.IntRange(0, 30).Draw(t, "keep")
		summary := rapid.String().Draw(t, "summary")

		m := NewManager()
		for i := 0; i < count; i++ {
			m.Append(api.UserMessage(fmt.Sprintf("m%d", i)))
		}

		before := len(m.Messages())
		m.Summarise(summary, keep)
		after := len(m.Messages())

		if before <= keep {
			if after != before {
				t.Fatalf("Expected %d messages after keeping %d (from %d), but got %d", before, keep, before, after)
			}
		} else {
			if after != keep+1 {
				t.Fatalf("Expected %d messages after summarising to keep %d, but got %d", keep+1, keep, after)
			}
		}
	})
}

func TestProperty_SetContextLimitPositiveOnly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		limit := rapid.Int().Draw(t, "limit")
		sys := rapid.String().Draw(t, "sys")

		m := NewManagerWithSystem(sys)
		prev := m.ContextLimit()
		m.SetContextLimit(limit)
		got := m.ContextLimit()

		if limit > 0 {
			if got != limit {
				t.Fatalf("Expected limit to be updated to %d, got %d", limit, got)
			}
		} else {
			if got != prev {
				t.Fatalf("Expected limit to remain %d when setting negative limit %d, got %d", prev, limit, got)
			}
		}
	})
}

func TestProperty_ResetClearsHistory(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 50).Draw(t, "n")
		s := rapid.String().Draw(t, "s")

		m := NewManagerWithSystem(s)
		for i := 0; i < n; i++ {
			m.Append(api.UserMessage(fmt.Sprintf("x%d", i)))
		}

		m.Reset()

		if len(m.Messages()) != 0 {
			t.Fatalf("Expected 0 messages after reset, got %d", len(m.Messages()))
		}
		if m.SystemPrompt() != s {
			t.Fatalf("Expected system prompt to remain %q, got %q", s, m.SystemPrompt())
		}
	})
}

