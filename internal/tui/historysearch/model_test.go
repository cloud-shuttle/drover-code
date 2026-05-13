package historysearch

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModel_DeduplicationAndOrder(t *testing.T) {
	entries := []string{"cmd1", "cmd2", "cmd1", "cmd3"}
	m := New(entries, 100, 50)

	// Items should be reversed and deduplicated: cmd3, cmd1, cmd2
	items := m.list.Items()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	if items[0].FilterValue() != "cmd3" || items[1].FilterValue() != "cmd1" || items[2].FilterValue() != "cmd2" {
		t.Fatalf("incorrect order or deduplication: %v", items)
	}
}

func TestModel_CancelMsg(t *testing.T) {
	entries := []string{"cmd1"}
	m := New(entries, 100, 50)

	// Sending Ctrl+C should return CancelMsg
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}
	msg := cmd()
	if _, ok := msg.(CancelMsg); !ok {
		t.Fatalf("expected CancelMsg, got %T", msg)
	}

	// Sending Esc while NOT filtering should return CancelMsg
	m.list.ResetFilter()
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}
	msg = cmd()
	if _, ok := msg.(CancelMsg); !ok {
		t.Fatalf("expected CancelMsg, got %T", msg)
	}
}

func TestModel_SelectedMsg(t *testing.T) {
	entries := []string{"cmd1", "cmd2"}
	m := New(entries, 100, 50)

	// Enter should return SelectedMsg
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}
	msg := cmd()
	sel, ok := msg.(SelectedMsg)
	if !ok {
		t.Fatalf("expected SelectedMsg, got %T", msg)
	}
	if sel.Entry != "cmd2" { // cmd2 is at the top because it's the newest
		t.Fatalf("expected cmd2, got %q", sel.Entry)
	}
}

func TestModel_WindowSize(t *testing.T) {
	entries := []string{"cmd1"}
	m := New(entries, 100, 50)
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 100})
	// Just ensuring it doesn't panic and processes properly.
	m.SetSize(300, 150)
}
