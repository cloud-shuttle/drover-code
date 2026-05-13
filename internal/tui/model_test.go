package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/api"
)

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
	m.textarea.SetValue("hello")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := next.(*Model)
	if got != "hello" {
		t.Fatalf("runFunc input %q", got)
	}
	if !m2.agentBusy {
		t.Fatal("expected agent busy after submit")
	}
	if m2.textarea.Value() != "" {
		t.Fatal("expected textarea cleared")
	}
}

func TestModel_agentTextDeltaAndDone(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.agentBusy = true
	m.streaming = true
	next, _ := m.Update(agentMsg{event: agent.TextDeltaEvent{Text: "hello"}})
	m2 := next.(*Model)
	if !strings.Contains(m2.streamBuf.String(), "hello") {
		t.Fatalf("buf %q", m2.streamBuf.String())
	}
	next, _ = m2.Update(agentMsg{event: agent.DoneEvent{}})
	m3 := next.(*Model)
	if len(m3.history) != 1 || m3.history[0].role != "assistant" {
		t.Fatalf("history %+v", m3.history)
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
	if m.permPrompt == nil {
		t.Fatal("expected perm prompt")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m2 := next.(*Model)
	if m2.permPrompt != nil {
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
	if m2.permPrompt != nil {
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
	if m3.permPrompt != nil {
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
	if m2.permPrompt != nil {
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
	if m3.permPrompt != nil {
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
	if m.permBatch == nil || m.permPrompt != nil {
		t.Fatalf("batch=%v prompt=%v", m.permBatch != nil, m.permPrompt)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m2 := next.(*Model)
	if m2.permBatch != nil {
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
	if m2.permBatch != nil {
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
	if m3.permBatch != nil {
		t.Fatal("expected batch cleared")
	}
	if d := <-dec2; d != agent.PermAlwaysAllow {
		t.Fatalf("always: got %v", d)
	}
}

func TestModel_slashAutocompleteTab(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.textarea.SetValue("/cl")
	m.updateAutoComplete()
	if !m.showAuto {
		t.Fatal("expected autocomplete open")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m2 := next.(*Model)
	if m2.showAuto {
		t.Fatal("expected autocomplete closed after tab")
	}
	if v := m2.textarea.Value(); v != "/clear " {
		t.Fatalf("textarea %q", v)
	}
}

func TestModel_slashAutocompleteArrowDown(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.textarea.SetValue("/")
	m.updateAutoComplete()
	if m.autoIndex != 0 {
		t.Fatalf("autoIndex %d", m.autoIndex)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m2 := next.(*Model)
	if m2.autoIndex != 1 {
		t.Fatalf("autoIndex after down: %d", m2.autoIndex)
	}
	next, _ = m2.Update(tea.KeyMsg{Type: tea.KeyUp})
	m3 := next.(*Model)
	if m3.autoIndex != 0 {
		t.Fatalf("autoIndex after up: %d", m3.autoIndex)
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
	m.streaming = true

	next, _ := m.Update(agentMsg{event: agent.TextDeltaEvent{Text: "Hi "}})
	m2 := next.(*Model)
	next, _ = m2.Update(agentMsg{event: agent.ToolStartEvent{
		CallIndex:    0,
		Name:         "bash",
		InputSummary: "echo",
	}})
	m3 := next.(*Model)
	if m3.activeTools[0] == nil {
		t.Fatal("expected active tool")
	}
	next, _ = m3.Update(agentMsg{event: agent.ToolDoneEvent{
		CallIndex:     0,
		Name:          "bash",
		OutputSummary: "ok",
		IsError:       false,
	}})
	m4 := next.(*Model)
	if len(m4.pendingDone) != 1 {
		t.Fatalf("pendingDone: %d", len(m4.pendingDone))
	}
	next, _ = m4.Update(agentMsg{event: agent.DoneEvent{}})
	m5 := next.(*Model)
	if len(m5.history) != 1 || m5.history[0].role != "assistant" {
		t.Fatalf("history %+v", m5.history)
	}
	if len(m5.history[0].tools) != 1 || m5.history[0].tools[0].name != "bash" {
		t.Fatalf("tools %+v", m5.history[0].tools)
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
	if len(m2.history) != 1 || !strings.Contains(m2.history[0].content, "/compact") {
		t.Fatalf("history %+v", m2.history)
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
	if len(m3.history) != 1 {
		t.Fatalf("error path should not append success history, got %d turns", len(m3.history))
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

	m.textarea.SetValue("first")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := next.(*Model)
	if len(m2.history) != 1 || m2.history[0].role != "user" || m2.history[0].content != "first" {
		t.Fatalf("after first submit: %+v", m2.history)
	}
	if !m2.agentBusy {
		t.Fatal("expected busy")
	}

	next, _ = m2.Update(agentMsg{event: agent.TextDeltaEvent{Text: "r1"}})
	m3 := next.(*Model)
	next, _ = m3.Update(agentMsg{event: agent.DoneEvent{}})
	m4 := next.(*Model)
	if len(m4.history) != 2 || m4.history[1].role != "assistant" {
		t.Fatalf("after first reply: n=%d %+v", len(m4.history), m4.history)
	}
	if !strings.Contains(m4.history[1].content, "r1") {
		t.Fatalf("assistant content %q", m4.history[1].content)
	}
	if m4.agentBusy {
		t.Fatal("expected idle before second turn")
	}

	m4.textarea.SetValue("second")
	next, _ = m4.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m5 := next.(*Model)
	if len(m5.history) != 3 || m5.history[2].content != "second" {
		t.Fatalf("after second submit: %+v", m5.history)
	}
	next, _ = m5.Update(agentMsg{event: agent.TextDeltaEvent{Text: "r2"}})
	m6 := next.(*Model)
	next, _ = m6.Update(agentMsg{event: agent.DoneEvent{}})
	m7 := next.(*Model)
	if len(m7.history) != 4 {
		t.Fatalf("want 4 turns, got %d", len(m7.history))
	}
	if m7.history[3].role != "assistant" || !strings.Contains(m7.history[3].content, "r2") {
		t.Fatalf("second assistant: %+v", m7.history[3])
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

	m.textarea.SetValue("task 1")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := next.(*Model)
	if len(runs) != 1 || runs[0] != "task 1" {
		t.Fatalf("first run %v", runs)
	}
	if !m2.agentBusy {
		t.Fatal("expected busy")
	}

	m2.textarea.SetValue("task 2")
	next, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := next.(*Model)
	
	if len(runs) != 1 {
		t.Fatalf("second run happened too early: %v", runs)
	}
	if len(m3.messageQueue) != 1 || m3.messageQueue[0] != "task 2" {
		t.Fatalf("expected queue to have 'task 2', got %v", m3.messageQueue)
	}
	
	next, _ = m3.Update(agentRunCompleteMsg{err: nil})
	m4 := next.(*Model)
	
	if len(runs) != 2 || runs[1] != "task 2" {
		t.Fatalf("expected runFunc to be called with 'task 2', got runs: %v", runs)
	}
	if len(m4.messageQueue) != 0 {
		t.Fatalf("expected queue to be empty, got %v", m4.messageQueue)
	}
	if !m4.agentBusy {
		t.Fatal("expected busy again since 'task 2' is running")
	}
}

func TestModel_InputHistory(t *testing.T) {
	evCh := make(chan agent.Event, 10)
	m := New(evCh, "test-model", "/tmp/wd", "user", "host")

	// Submit first message
	m.textarea.SetValue("message 1")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Submit second message
	m.textarea.SetValue("message 2")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Type a partial message
	m.textarea.SetValue("partial")

	// Press Up
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.textarea.Value() != "message 2" {
		t.Fatalf("expected message 2, got %q", m.textarea.Value())
	}

	// Press Up again
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.textarea.Value() != "message 1" {
		t.Fatalf("expected message 1, got %q", m.textarea.Value())
	}

	// Press Down
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.textarea.Value() != "message 2" {
		t.Fatalf("expected message 2, got %q", m.textarea.Value())
	}

	// Press Down to restore saved input
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.textarea.Value() != "partial" {
		t.Fatalf("expected partial, got %q", m.textarea.Value())
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
		mod.textarea.SetValue(inputStr)
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
	if len(finalModel.history) < 1000 { // 500 user messages + 500 agent messages
		t.Fatalf("expected at least 1000 messages in history, got %d", len(finalModel.history))
	}

	// Just a sanity check that it successfully runs without panicking.
	_ = finalModel.View()
}
