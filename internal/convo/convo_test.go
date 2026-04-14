package convo

import (
	"strings"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/api"
)

func TestManager_CharsPerToken(t *testing.T) {
	sys := strings.Repeat("a", 400)
	m := NewManagerWithSystem(sys)
	if got := m.EstimatedTokens(); got != 100 {
		t.Fatalf("default divisor: got %d want 100", got)
	}
	m.SetCharsPerToken(5)
	if got := m.EstimatedTokens(); got != 80 {
		t.Fatalf("divisor 5: got %d want 80", got)
	}
	if m.CharsPerToken() != 5 {
		t.Fatalf("CharsPerToken: %d", m.CharsPerToken())
	}
	m.SetCharsPerToken(99) // ignored
	if m.CharsPerToken() != 5 {
		t.Fatal("invalid SetCharsPerToken should not apply")
	}
}

func TestManager_LastUserContentBreakdown(t *testing.T) {
	m := NewManagerWithSystem(strings.Repeat("s", 40))
	m.Append(api.UserMessage("user says"))
	m.Append(api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: "assistant"}}))
	m.Append(api.Message{
		Role: api.RoleUser,
		Content: []api.ContentBlock{
			api.TextBlock{Text: strings.Repeat("u", 40)},
			api.ToolResultBlock{ToolUseID: "x", Content: strings.Repeat("r", 80)},
		},
	})
	txt, tr := m.LastUserContentBreakdown()
	if wantT, wantR := 40/DefaultCharsPerToken, 80/DefaultCharsPerToken; txt != wantT || tr != wantR {
		t.Fatalf("text=%d toolRes=%d want %d,%d", txt, tr, wantT, wantR)
	}
}

func TestManager_Reset_ClearsMessagesAndCalibration(t *testing.T) {
	m := NewManagerWithSystem("sys")
	m.Append(api.UserMessage("hello"))
	m.RecordAPICalibration(1000, 1000)
	if _, _, _, ok := m.CalibrationHint(); !ok {
		t.Fatal("expected calibration after RecordAPICalibration")
	}
	if len(m.Messages()) != 1 {
		t.Fatalf("messages: %d", len(m.Messages()))
	}

	m.Reset()
	if len(m.Messages()) != 0 {
		t.Fatalf("after Reset messages: %d", len(m.Messages()))
	}
	if _, _, _, ok := m.CalibrationHint(); ok {
		t.Fatal("Reset should clear calibration state")
	}
}
