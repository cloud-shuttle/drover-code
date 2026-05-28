package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) View() string {
	if m.showingSearch && m.searchModel != nil {
		return m.searchModel.View()
	}
	
	if m.showingDiff && m.diffModel != nil {
		return m.diffModel.View()
	}

	if m.showingCommandPalette && m.commandPaletteModel != nil {
		return m.commandPaletteModel.View()
	}

	if m.width == 0 {
		return "loading…"
	}

	var sections []string

	// HistoryView is the sole owner of conversation history
	if m.HistoryView != nil && m.HistoryView.Len() == 0 && !m.agentBusy && !m.Live.Streaming {
		sections = append(sections, m.viewHome())
	} else {
		sections = append(sections, m.HistoryView.View())
	}

	// dcode-004: LiveRegion component is the source of truth
	if live := m.Live.View(); live != "" {
		sections = append(sections, live)
	}

	if m.lastError != "" {
		sections = append(sections, styleError.Width(m.width-2).Render("error: "+m.lastError))
	}

	if m.compactionBanner != "" {
		sections = append(sections, lipgloss.NewStyle().Foreground(colSubtle).Width(m.width-2).Render(m.compactionBanner))
	}

	// dcode-003: StatusBar component is the source of truth
	sections = append(sections, m.StatusBar.View())


	// dcode-007: prefer new PermissionPrompt components (old perm* render paths removed as dead code)
	if m.PermPrompt != nil {
		m.PermPrompt.Width = m.width
		sections = append(sections, m.PermPrompt.View())
	} else if m.PermBatch != nil {
		m.PermBatch.Width = m.width
		sections = append(sections, m.PermBatch.View())
	} else if m.InputArea != nil {
		// dcode-009: InputArea component owns the visual input region
		sections = append(sections, m.InputArea.View())
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m *Model) viewHome() string {
	// Roughly mimic the “welcome + tips” split pane.
	w := m.width
	if w < 60 {
		w = 60
	}
	innerW := w - 6
	if innerW < 50 {
		innerW = 50
	}

	leftW := innerW/3 + 2
	rightW := innerW - leftW - 2
	if rightW < 30 {
		rightW = 30
	}

	mark := styleHomeBrand.Render(brandMark(leftW - 4))

	left := lipgloss.JoinVertical(lipgloss.Left,
		styleHomeWelcome.Render("Welcome back "+m.userName+"!"),
		"",
		mark,
		"",
		styleHomeMeta.Render("DROVER CODE"),
		"",
		styleHomeMeta.Render("Model: "+m.modelName),
		styleHomeMeta.Render("Project: "+m.workDir),
		styleHomeMeta.Render("Host: "+m.hostName),
	)
	left = lipgloss.NewStyle().Width(leftW).Render(left)

	right := lipgloss.JoinVertical(lipgloss.Left,
		styleHomeTipsHeader.Render("Tips for getting started"),
		styleHomeTipsBody.Render("Run `/init` to add a project instructions file (CLAUDE.md)."),
		"",
		styleHomeTipsHeader.Render("Recent activity"),
		styleHomeTipsBody.Render("No recent activity"),
	)
	right = lipgloss.NewStyle().Width(rightW).Render(right)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)

	title := styleHomeTitle.Render(" drover-code ")
	// Put title “inside” the box by rendering it above; simple and reliable.
	return lipgloss.JoinVertical(lipgloss.Left,
		styleHomeTitle.Width(m.width).Render(title),
		styleHomeBox.Width(m.width-2).Render(body),
	)
}



func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

func formatTokens(n int) string {
	switch {
	case n == 0:
		return "0"
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 10_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	case n < 1_000_000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	}
}

