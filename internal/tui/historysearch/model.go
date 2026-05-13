package historysearch

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	docStyle = lipgloss.NewStyle().Margin(1, 2)
)

// SelectedMsg is fired when the user selects a command from the history search.
type SelectedMsg struct {
	Entry string
}

// CancelMsg is fired when the user escapes the history search.
type CancelMsg struct{}

// historyItem represents a single history entry in the list.
type historyItem string

func (i historyItem) Title() string       { return string(i) }
func (i historyItem) Description() string { return "Command History" }
func (i historyItem) FilterValue() string { return string(i) }

// Model is the BubbleTea model for the fuzzy history search.
type Model struct {
	list list.Model
}

// New creates a new history search model initialized with the given history entries.
func New(entries []string, width, height int) *Model {
	// Deduplicate and reverse entries so the newest are at the top
	seen := make(map[string]bool)
	var items []list.Item
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if !seen[entry] && entry != "" {
			seen[entry] = true
			items = append(items, historyItem(entry))
		}
	}

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false // Hide description to keep it compact

	m := list.New(items, delegate, width, height)
	m.Title = "Fuzzy History Search (Ctrl+R)"
	m.SetShowStatusBar(true)
	m.SetFilteringEnabled(true)
	m.Styles.Title = lipgloss.NewStyle().Background(lipgloss.Color("62")).Foreground(lipgloss.Color("230")).Padding(0, 1)

	// Force into filtering mode by simulating a '/' keypress
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})

	return &Model{
		list: m,
	}
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// If filtering is not active, allow the user to easily escape.
		// bubbles/list captures enter when selecting an item.
		if msg.String() == "ctrl+c" {
			return m, func() tea.Msg { return CancelMsg{} }
		}

		// List also captures Enter to select, but we can intercept or let it handle it.
		// If the user presses enter, we emit the selected message.
		if msg.String() == "enter" {
			i, ok := m.list.SelectedItem().(historyItem)
			if ok {
				return m, func() tea.Msg { return SelectedMsg{Entry: string(i)} }
			}
		}

		// If the user escapes from the list's normal mode (not filtering), cancel out.
		if msg.String() == "esc" && m.list.FilterState() != list.Filtering {
			return m, func() tea.Msg { return CancelMsg{} }
		}
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *Model) View() string {
	return docStyle.Render(m.list.View())
}

// SetSize updates the list size
func (m *Model) SetSize(width, height int) {
	h, v := docStyle.GetFrameSize()
	m.list.SetSize(width-h, height-v)
}
