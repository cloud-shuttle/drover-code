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
	if !ok || len(m.history) != 1 {
		t.Fatalf("tokens: ok=%v history=%d", ok, len(m.history))
	}
	if m.history[0].role != "user" || !strings.Contains(m.history[0].content, "my-model") {
		t.Fatalf("content %q", m.history[0].content)
	}

	m.history = nil
	_, ok = m.handleBuiltinSlash("/model")
	if !ok || len(m.history) != 1 || !strings.Contains(m.history[0].content, "my-model") {
		t.Fatalf("model: %+v", m.history)
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
	if !ok || len(m.history) != 1 {
		t.Fatalf("ok=%v history=%d", ok, len(m.history))
	}
	if !strings.Contains(m.history[0].content, "usage") || !strings.Contains(m.history[0].content, "/plan") {
		t.Fatalf("content %q", m.history[0].content)
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
	if !m.agentBusy || !m.streaming {
		t.Fatalf("busy=%v streaming=%v", m.agentBusy, m.streaming)
	}
	if len(m.history) != 1 || m.history[0].content != "/plan design/ADR.md" {
		t.Fatalf("history %+v", m.history[0])
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
	if m.agentBusy || m.streaming {
		t.Fatal("should not mark busy without runFunc")
	}
	if m.lastError != "agent not wired" {
		t.Fatalf("lastError %q", m.lastError)
	}
}
