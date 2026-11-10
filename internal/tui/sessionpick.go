package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	liveregion "github.com/cloudshuttle/drover-code/internal/tui/components/liveregion"
)

// SessionRow is one line in the resume picker.
type SessionRow struct {
	ID      string
	When    string
	Preview string
}

func (m *Model) updateSessionPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return m, tea.Quit
	case "up", "ctrl+p":
		m.clampSessionCursor(len(m.visibleSessions()))
		if m.sessionCursor > 0 {
			m.sessionCursor--
		}
		return m, nil
	case "down", "ctrl+n":
		n := len(m.visibleSessions())
		m.clampSessionCursor(n)
		if m.sessionCursor < n-1 {
			m.sessionCursor++
		}
		return m, nil
	case "enter":
		rows := m.visibleSessions()
		m.clampSessionCursor(len(rows))
		if len(rows) == 0 || m.loadSession == nil {
			return m, nil
		}
		if err := m.loadSession(rows[m.sessionCursor].ID); err != nil {
			m.lastError = err.Error()
			return m, nil
		}
		m.showingSessions = false
		return m, nil
	case "backspace":
		runes := []rune(m.sessionQuery)
		if len(runes) > 0 {
			m.setSessionQuery(string(runes[:len(runes)-1]))
		}
		return m, nil
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
		m.setSessionQuery(m.sessionQuery + string(msg.Runes))
	}
	return m, nil
}

func (m *Model) setSessionQuery(q string) {
	stored, _ := liveregion.StripInputCursorReports(q)
	m.sessionQuery = stored
	m.sessionCursor = 0
}

func (m *Model) sessionFilterText() string {
	_, shown := liveregion.StripInputCursorReports(m.sessionQuery)
	return shown
}

func (m *Model) visibleSessions() []SessionRow {
	q := strings.ToLower(strings.TrimSpace(m.sessionFilterText()))
	if q == "" {
		return m.sessionRows
	}
	var out []SessionRow
	for _, row := range m.sessionRows {
		hay := strings.ToLower(row.ID + " " + row.Preview)
		if strings.Contains(hay, q) {
			out = append(out, row)
		}
	}
	return out
}

func (m *Model) clampSessionCursor(n int) {
	if n <= 0 || m.sessionCursor < 0 {
		m.sessionCursor = 0
		return
	}
	if m.sessionCursor >= n {
		m.sessionCursor = n - 1
	}
}

func (m *Model) viewSessions() string {
	rows := m.visibleSessions()
	m.clampSessionCursor(len(rows))
	width := m.width
	if width < 40 {
		width = 40
	}
	var lines []string
	lines = append(lines,
		lipgloss.NewStyle().Bold(true).Render("Resume a session"),
		"up/down move    enter continue    esc quit    type to filter",
		"filter: "+m.sessionFilterText(),
		"",
	)
	if len(rows) == 0 {
		lines = append(lines, "No sessions match.")
		return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
	}
	const window = 12
	start := 0
	if m.sessionCursor >= window {
		start = m.sessionCursor - window + 1
	}
	end := start + window
	if end > len(rows) {
		end = len(rows)
	}
	for i := start; i < end; i++ {
		row := rows[i]
		preview := row.Preview
		if preview == "" {
			preview = "(no message)"
		}
		line := fmt.Sprintf("%s  %s  %s", row.ID, row.When, preview)
		if i == m.sessionCursor {
			line = "> " + line
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}
