package diff

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	fileStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	hunkHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true)
	selectedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	acceptedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	rejectedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	addedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	removedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	contextStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

type Model struct {
	viewport viewport.Model
	diff     DiffModel
	ready    bool
}

func NewDiffModel(filePath string, unifiedDiff string) Model {
	hunks, _ := ParseUnifiedDiff(unifiedDiff) // error handled gracefully in parser

	m := Model{
		diff: DiffModel{
			FilePath: filePath,
			Hunks:    hunks,
			Cursor:   0,
		},
		viewport: viewport.New(100, 28),
		ready:    true,
	}

	m.viewport.SetContent(m.renderFullDiff())
	return m
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if m.diff.Cursor < len(m.diff.Hunks)-1 {
				m.diff.Cursor++
			}
		case "k", "up":
			if m.diff.Cursor > 0 {
				m.diff.Cursor--
			}
		case " ", "t": // Toggle
			if idx := m.diff.Cursor; idx < len(m.diff.Hunks) {
				h := &m.diff.Hunks[idx]
				h.Accepted = !h.Accepted
				h.Rejected = false
			}
		case "a", "A": // Accept all
			for i := range m.diff.Hunks {
				m.diff.Hunks[i].Accepted = true
				m.diff.Hunks[i].Rejected = false
			}
		case "r", "R": // Reject all
			for i := range m.diff.Hunks {
				m.diff.Hunks[i].Accepted = false
				m.diff.Hunks[i].Rejected = true
			}
		case "c", "C": // Clear selection
			for i := range m.diff.Hunks {
				m.diff.Hunks[i].Accepted = false
				m.diff.Hunks[i].Rejected = false
			}
		}
	}

	m.viewport.SetContent(m.renderFullDiff())
	m.viewport, cmd = m.viewport.Update(msg)

	return m, cmd
}

func (m Model) View() string {
	return m.viewport.View() + "\n\n" + m.renderHelpBar()
}

func (m Model) renderFullDiff() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Interactive Diff Review") + "\n")
	b.WriteString(fileStyle.Render("File: "+m.diff.FilePath) + "\n")
	b.WriteString(strings.Repeat("─", 90) + "\n\n")

	for i, hunk := range m.diff.Hunks {
		isSelected := i == m.diff.Cursor

		// Hunk header
		prefix := "   "
		if isSelected {
			prefix = selectedStyle.Render("→ ")
		}

		status := " "
		if hunk.Accepted {
			status = acceptedStyle.Render("✓ ACCEPTED")
		} else if hunk.Rejected {
			status = rejectedStyle.Render("✗ REJECTED")
		}

		b.WriteString(fmt.Sprintf("%s%s %s\n", prefix, status, hunkHeaderStyle.Render(hunk.Header)))

		// Content preview (limited lines)
		lines := append(append(hunk.OldContent, hunk.NewContent...), hunk.Context...)
		maxLines := 6 // Show a reasonable preview
		for j, line := range lines {
			if j >= maxLines {
				b.WriteString("    ...\n")
				break
			}

			trimmed := strings.TrimPrefix(line, " ")
			if len(trimmed) > 80 {
				trimmed = trimmed[:77] + "..."
			}

			if strings.HasPrefix(line, "+") {
				b.WriteString(addedStyle.Render("    + "+trimmed) + "\n")
			} else if strings.HasPrefix(line, "-") {
				b.WriteString(removedStyle.Render("    - "+trimmed) + "\n")
			} else {
				b.WriteString(contextStyle.Render("    "+trimmed) + "\n")
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderHelpBar() string {
	return helpStyle.Render(
		"↑↓/jk Navigate • Space Toggle • A Accept All • R Reject All • C Clear • Enter Confirm • Q Quit",
	)
}

func (m Model) GetHunks() []Hunk {
	return m.diff.Hunks
}

func (m Model) GetFilePath() string {
	return m.diff.FilePath
}
