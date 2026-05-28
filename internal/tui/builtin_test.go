package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/convo"
)

func TestHandleBuiltinSlash_tokensAndModel(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "my-model", "/work", "u", "h")
	m.SetConversation(convo.NewManagerWithSystem("sys"))
	m.totalInputTokens = 10
	m.totalOutputTokens = 20

	_, ok := m.handleBuiltinSlash("/tokens")
	histLen := m.HistoryView.Len()
	var firstContent string
	if histLen > 0 {
		firstContent = m.HistoryView.GetTurns()[0].Content
	}
	if !ok || histLen != 1 {
		t.Fatalf("tokens: ok=%v history=%d", ok, histLen)
	}
	if !strings.Contains(firstContent, "my-model") {
		t.Fatalf("content %q", firstContent)
	}

	m.HistoryView.Clear()
	_, ok = m.handleBuiltinSlash("/model")
	histLen = m.HistoryView.Len()
	firstContent = ""
	if histLen > 0 {
		firstContent = m.HistoryView.GetTurns()[0].Content
	}
	if !ok || histLen != 1 || !strings.Contains(firstContent, "my-model") {
		t.Fatalf("model: history len=%d content=%q", histLen, firstContent)
	}
}

func TestHandleBuiltinSlash_clearResetsConvo(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	mgr := convo.NewManager()
	mgr.Append(api.UserMessage("hello"))
	m.SetConversation(mgr)

	_, ok := m.handleBuiltinSlash("/clear")
	if !ok {
		t.Fatal("expected handled")
	}
	if len(mgr.Messages()) != 0 {
		t.Fatalf("messages should clear, got %d", len(mgr.Messages()))
	}
}

func TestHandleBuiltinSlash_compact(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.SetCompactFn(func() error { return nil })

	cmd, ok := m.handleBuiltinSlash("/compact")
	if !ok || cmd == nil {
		t.Fatal("expected compact cmd")
	}
	if !m.agentBusy {
		t.Fatal("expected agent busy during compact")
	}
}

func TestHandleBuiltinSlash_quit(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	cmd, ok := m.handleBuiltinSlash("/quit")
	if !ok || cmd == nil {
		t.Fatal("quit")
	}
	if cmd() == nil {
		t.Fatal("expected quit message")
	}
}

func TestHandleBuiltinSlash_planUsage(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	_, ok := m.handleBuiltinSlash("/plan")
	histLen := m.HistoryView.Len()
	var content string
	if histLen > 0 {
		content = m.HistoryView.GetTurns()[0].Content
	}
	if !ok || histLen != 1 {
		t.Fatalf("ok=%v history=%d", ok, histLen)
	}
	if !strings.Contains(content, "usage") || !strings.Contains(content, "/plan") {
		t.Fatalf("content %q", content)
	}
}

func TestHandleBuiltinSlash_planRunsAgentWhenWired(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	var got string
	m.runFunc = func(input string) tea.Cmd {
		got = input
		return nil
	}
	_, ok := m.handleBuiltinSlash("/plan design/ADR.md")
	if !ok {
		t.Fatal("expected handled")
	}
	if got == "" || !strings.Contains(got, "design/ADR.md") || !strings.Contains(got, "write_file") {
		t.Fatalf("prompt %q", got)
	}
	if !m.agentBusy || !m.Live.Streaming {
		t.Fatalf("busy=%v streaming=%v", m.agentBusy, m.Live.Streaming)
	}
	if m.HistoryView.Len() != 1 || m.HistoryView.GetTurns()[0].Content != "/plan design/ADR.md" {
		t.Fatalf("history %+v", m.HistoryView.GetTurns())
	}
}

func TestHandleBuiltinSlash_planWithTopicSecondToken(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	var got string
	m.runFunc = func(input string) tea.Cmd {
		got = input
		return nil
	}
	_, ok := m.handleBuiltinSlash("/plan out/PLAN.md authz and rollout")
	if !ok {
		t.Fatal("expected handled")
	}
	if !strings.Contains(got, "out/PLAN.md") || !strings.Contains(got, "authz and rollout") {
		t.Fatalf("prompt %q", got)
	}
}

func TestHandleBuiltinSlash_planNotWiredSetsError(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.runFunc = nil
	_, ok := m.handleBuiltinSlash("/plan x.md")
	if !ok {
		t.Fatal("expected handled")
	}
	if m.agentBusy || m.Live.Streaming {
		t.Fatal("should not mark busy without runFunc")
	}
	if m.lastError != "agent not wired" {
		t.Fatalf("lastError %q", m.lastError)
	}
}
