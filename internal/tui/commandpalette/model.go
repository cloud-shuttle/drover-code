package commandpalette

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	docStyle = lipgloss.NewStyle().Margin(1, 2)
)

// SelectedMsg is fired when the user selects a command from the palette.
type SelectedMsg struct {
	Name      string
	ActionKey string // non-empty means this is a semantic action, not text injection
}

// CancelMsg is fired when the user escapes the command palette.
type CancelMsg struct{}

// commandItem represents one entry in the command palette (internal list item).
type commandItem struct {
	name      string
	desc      string
	actionKey string
	category  string
	shortcut  string
	riskLevel string
}

func (i commandItem) Title() string {
	title := "/" + i.name
	if i.shortcut != "" {
		title += "  " + i.shortcut
	}
	return title
}

func (i commandItem) Description() string {
	if i.category != "" || i.riskLevel != "" {
		extra := ""
		if i.category != "" {
			extra += "[" + i.category + "]"
		}
		if i.riskLevel != "" && i.riskLevel != "normal" {
			if extra != "" {
				extra += " "
			}
			extra += "(" + i.riskLevel + ")"
		}
		if extra != "" {
			return extra + " " + i.desc
		}
	}
	return i.desc
}

func (i commandItem) FilterValue() string { return i.name + " " + i.desc + " " + i.category }

// Model is the BubbleTea model for the command palette.
type Model struct {
	list list.Model
}

// New creates a palette from simple name/desc pairs (text commands only).
// For semantic actions, prefer NewWithCommands.
func New(commands []struct{ Name, Desc string }, width, height int) *Model {
	cmds := make([]Command, len(commands))
	for i, c := range commands {
		cmds[i] = Command{Name: c.Name, Description: c.Desc}
	}
	return NewWithCommands(cmds, width, height)
}

// NewWithCommands creates a palette that can contain both text commands
// and semantic actions (via Command.ActionKey).
//
// Semantic actions (ActionKey != "") are executed directly by the main Model
// instead of injecting text into the textarea. This enables first-class
// actions like "compact", "clear", etc. without going through the agent loop.
func NewWithCommands(commands []Command, width, height int) *Model {
	items := make([]list.Item, len(commands))
	for i, c := range commands {
		items[i] = commandItem{
			name:      c.Name,
			desc:      c.Description,
			actionKey: c.ActionKey,
			category:  c.Category,
			shortcut:  c.Shortcut,
			riskLevel: c.RiskLevel,
		}
	}

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true

	m := list.New(items, delegate, width, height)
	m.Title = "Command Palette (Ctrl+K)"

	return &Model{list: m}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if selected, ok := m.list.SelectedItem().(commandItem); ok {
				return m, func() tea.Msg {
					return SelectedMsg{
						Name:      selected.name,
						ActionKey: selected.actionKey,
					}
				}
			}
		case "esc", "ctrl+c":
			return m, func() tea.Msg {
				return CancelMsg{}
			}
		}
	}

	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *Model) View() string {
	return docStyle.Render(m.list.View())
}

func (m *Model) SetSize(width, height int) {
	m.list.SetSize(width, height)
}