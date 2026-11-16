package commandpalette

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCommand_IsSemantic(t *testing.T) {
	tests := []struct {
		name string
		cmd  Command
		want bool
	}{
		{
			name: "text command has no ActionKey",
			cmd:  Command{Name: "help", Description: "show help"},
			want: false,
		},
		{
			name: "semantic action has ActionKey",
			cmd: Command{
				Name:        "clear",
				Description: "Clear history",
				ActionKey:   "clear",
			},
			want: true,
		},
		{
			name: "semantic action with rich metadata",
			cmd: Command{
				Name:        "compact",
				Description: "Compress context",
				ActionKey:   "compact",
				Category:    "Agent",
				Shortcut:    "⌘K C",
				RiskLevel:   "caution",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cmd.IsSemantic(); got != tt.want {
				t.Errorf("IsSemantic() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCommandItem_TitleAndDescription(t *testing.T) {
	tests := []struct {
		name            string
		item            commandItem
		wantTitlePart   string
		wantDescParts   []string
		notWantInDesc   []string
	}{
		{
			name: "simple text command",
			item: commandItem{
				name: "help",
				desc: "Show available commands",
			},
			wantTitlePart: "/help",
			wantDescParts: []string{"Show available commands"},
		},
		{
			name: "command with shortcut",
			item: commandItem{
				name:     "clear",
				desc:     "Clear conversation",
				shortcut: "⌘K X",
			},
			wantTitlePart: "⌘K X",
			wantDescParts: []string{"Clear conversation"},
		},
		{
			name: "semantic action with category and risk",
			item: commandItem{
				name:      "compact",
				desc:      "Summarise context",
				actionKey: "compact",
				category:  "Agent",
				riskLevel: "caution",
			},
			wantTitlePart: "/compact",
			wantDescParts: []string{"[Agent]", "(caution)", "Summarise context"},
		},
		{
			name: "risk normal is omitted from description",
			item: commandItem{
				name:      "tokens",
				desc:      "Show token usage",
				category:  "TUI",
				riskLevel: "normal",
			},
			wantTitlePart: "/tokens",
			wantDescParts: []string{"[TUI]", "Show token usage"},
			notWantInDesc: []string{"(normal)"},
		},
		{
			name: "high risk is shown",
			item: commandItem{
				name:      "clear",
				desc:      "Clear everything",
				category:  "TUI",
				riskLevel: "high",
			},
			wantTitlePart: "/clear",
			wantDescParts: []string{"[TUI]", "(high)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title := tt.item.Title()
			if !strings.Contains(title, tt.wantTitlePart) {
				t.Errorf("Title() = %q, expected to contain %q", title, tt.wantTitlePart)
			}

			desc := tt.item.Description()
			for _, part := range tt.wantDescParts {
				if !strings.Contains(desc, part) {
					t.Errorf("Description() = %q, expected to contain %q", desc, part)
				}
			}
			for _, part := range tt.notWantInDesc {
				if strings.Contains(desc, part) {
					t.Errorf("Description() = %q, did not expect %q", desc, part)
				}
			}
		})
	}
}

func TestCommandItem_FilterValue(t *testing.T) {
	item := commandItem{
		name:     "compact",
		desc:     "Compress conversation history",
		category: "Agent",
	}
	fv := item.FilterValue()
	if !strings.Contains(fv, "compact") || !strings.Contains(fv, "Compress") || !strings.Contains(fv, "Agent") {
		t.Errorf("FilterValue() = %q, expected to contain name, desc and category", fv)
	}
}

func TestNewWithCommands_Basic(t *testing.T) {
	cmds := []Command{
		{Name: "help", Description: "Show help"},
		{Name: "clear", Description: "Clear history", ActionKey: "clear", Category: "TUI", RiskLevel: "caution"},
	}

	m := NewWithCommands(cmds, 80, 20)
	if m == nil {
		t.Fatal("NewWithCommands returned nil")
	}

	v := m.View()
	if v == "" {
		t.Error("expected non-empty View")
	}
	if !strings.Contains(v, "/help") || !strings.Contains(v, "/clear") {
		t.Errorf("View missing expected commands: %q", v)
	}
	if !strings.Contains(v, "[TUI]") || !strings.Contains(v, "(caution)") {
		t.Errorf("View missing rich metadata for semantic action: %q", v)
	}
}

func TestNew_SimpleWrapper(t *testing.T) {
	simple := []struct{ Name, Desc string }{
		{"foo", "bar"},
	}
	m := New(simple, 60, 15)
	if m == nil {
		t.Fatal("New returned nil")
	}
	if !strings.Contains(m.View(), "/foo") {
		t.Error("expected simple command rendered")
	}
}

func TestModel_SetSize(t *testing.T) {
	m := NewWithCommands([]Command{{Name: "x", Description: "y"}}, 40, 10)
	m.SetSize(100, 30)
	// No direct getters, but should not panic and View should still work
	v := m.View()
	if v == "" {
		t.Error("View empty after SetSize")
	}
}

func TestModel_Update_EnterProducesSelectedMsg(t *testing.T) {
	cmds := []Command{
		{Name: "tokens", Description: "Show tokens", ActionKey: "tokens"},
	}
	m := NewWithCommands(cmds, 80, 20)

	// Simulate enter key
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if updated == nil {
		t.Fatal("Update returned nil model")
	}

	if cmd == nil {
		t.Fatal("expected a Cmd from enter on selection")
	}

	// Execute the returned Cmd to get the message
	msg := cmd()

	sel, ok := msg.(SelectedMsg)
	if !ok {
		t.Fatalf("expected SelectedMsg, got %T", msg)
	}
	if sel.Name != "tokens" || sel.ActionKey != "tokens" {
		t.Errorf("SelectedMsg = %+v, want Name=tokens ActionKey=tokens", sel)
	}
}

func TestModel_Update_EscProducesCancelMsg(t *testing.T) {
	m := NewWithCommands([]Command{{Name: "x", Description: "y"}}, 80, 10)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected Cmd from esc")
	}

	msg := cmd()
	if _, ok := msg.(CancelMsg); !ok {
		t.Fatalf("expected CancelMsg, got %T", msg)
	}
}

func TestModel_Update_CtrlCProducesCancelMsg(t *testing.T) {
	m := NewWithCommands([]Command{{Name: "x", Description: "y"}}, 80, 10)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected Cmd from ctrl+c")
	}

	msg := cmd()
	if _, ok := msg.(CancelMsg); !ok {
		t.Fatalf("expected CancelMsg, got %T", msg)
	}
}

func TestModel_View_ContainsTitle(t *testing.T) {
	m := NewWithCommands([]Command{{Name: "init", Description: "Initialize project"}}, 80, 15)
	v := m.View()
	if !strings.Contains(v, "Command Palette") {
		t.Errorf("View should contain palette title, got: %q", v)
	}
}

func TestModel_View_NarrowWidth(t *testing.T) {
	cmds := []Command{
		{Name: "very-long-command-name-that-might-wrap", Description: "A description that is also fairly long"},
	}
	m := NewWithCommands(cmds, 25, 10) // deliberately narrow
	v := m.View()
	// Should still render something without crashing
	if v == "" {
		t.Error("expected non-empty view even on narrow width")
	}
}