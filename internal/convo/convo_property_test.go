package convo

import (
	"fmt"
	"testing"
	"testing/quick"

	"github.com/cloudshuttle/drover-code/internal/api"
)

func TestProperty_EstimatedTokensNonNegative(t *testing.T) {
	err := quick.Check(func(sys, body string) bool {
		m := NewManagerWithSystem(sys)
		m.Append(api.UserMessage(body))
		m.Append(api.AssistantMessage([]api.ContentBlock{
			api.TextBlock{Text: body},
			api.ToolUseBlock{ID: "x", Name: "n", Input: []byte(`{}`)},
		}))
		m.Append(api.ToolResultMessage([]api.ToolResultBlock{
			{ToolUseID: "x", Content: body},
		}))
		return m.EstimatedTokens() >= 0
	}, &quick.Config{MaxCount: 200})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProperty_SummariseMessageCount(t *testing.T) {
	err := quick.Check(func(n, k byte, summary string) bool {
		count := int(n%30) + 1
		keep := int(k % 30)
		m := NewManager()
		for i := 0; i < count; i++ {
			m.Append(api.UserMessage(fmt.Sprintf("m%d", i)))
		}
		before := len(m.Messages())
		m.Summarise(summary, keep)
		after := len(m.Messages())
		if before <= keep {
			return after == before
		}
		return after == keep+1
	}, &quick.Config{MaxCount: 200})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProperty_SetContextLimitPositiveOnly(t *testing.T) {
	err := quick.Check(func(limit int, sys string) bool {
		m := NewManagerWithSystem(sys)
		prev := m.ContextLimit()
		m.SetContextLimit(limit)
		got := m.ContextLimit()
		if limit > 0 {
			return got == limit
		}
		return got == prev
	}, &quick.Config{MaxCount: 150})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProperty_ResetClearsHistory(t *testing.T) {
	err := quick.Check(func(n byte, s string) bool {
		m := NewManagerWithSystem(s)
		for i := 0; i < int(n%50); i++ {
			m.Append(api.UserMessage(fmt.Sprintf("x%d", i)))
		}
		m.Reset()
		return len(m.Messages()) == 0 && m.SystemPrompt() == s
	}, &quick.Config{MaxCount: 150})
	if err != nil {
		t.Fatal(err)
	}
}
