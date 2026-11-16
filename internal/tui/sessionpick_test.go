package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/api"
)

func TestSessionPick_FilterAndSelect(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.showingSessions = true
	m.sessionRows = []SessionRow{
		{ID: "session-aaa", When: "2026-11-01 01:00", Preview: "read the notes"},
		{ID: "session-bbb", When: "2026-11-02 01:00", Preview: "fix the test"},
	}
	var got string
	m.loadSession = func(id string) error {
		got = id
		m.HistoryView.SetTurns(turnsFromMessages([]api.Message{api.UserMessage("fix the test")}))
		return nil
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix")})
	vis := m.visibleSessions()
	if len(vis) != 1 || vis[0].ID != "session-bbb" {
		t.Fatalf("filter = %+v", vis)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.showingSessions {
		t.Fatal("picker should close")
	}
	if got != "session-bbb" {
		t.Fatalf("selected %q", got)
	}
	if m.HistoryView.Len() != 1 {
		t.Fatalf("history len = %d", m.HistoryView.Len())
	}
}

func TestSessionPick_CursorReportIsNotAFilter(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.showingSessions = true
	m.sessionRows = []SessionRow{
		{ID: "session-aaa", Preview: "read the notes"},
		{ID: "session-bbb", Preview: "fix the test"},
	}

	feed := func(s string) {
		t.Helper()
		for _, r := range s {
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}

	feed(`\[1;1R`)
	if m.sessionFilterText() != "" {
		t.Fatalf("filter = %q", m.sessionFilterText())
	}
	if got := m.visibleSessions(); len(got) != 2 {
		t.Fatalf("rows after report = %d", len(got))
	}
	if !strings.Contains(m.viewSessions(), "session-aaa") {
		t.Fatalf("view hid the list:\n%s", m.viewSessions())
	}

	feed("[1;1R")
	if m.sessionFilterText() != "" || len(m.visibleSessions()) != 2 {
		t.Fatalf("bare report filter=%q rows=%d", m.sessionFilterText(), len(m.visibleSessions()))
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix")})
	vis := m.visibleSessions()
	if len(vis) != 1 || vis[0].ID != "session-bbb" {
		t.Fatalf("filter after report = %+v", vis)
	}
}

func TestSessionPick_EscapeQuits(t *testing.T) {
	ch := make(chan agent.Event, 1)
	m := New(ch, "m", "/w", "u", "h")
	m.showingSessions = true
	m.sessionRows = []SessionRow{{ID: "session-aaa", Preview: "hi"}}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("cmd result %T", cmd())
	}
}
