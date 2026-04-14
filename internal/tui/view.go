package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) View() string {
	if m.width == 0 {
		return "loading…"
	}

	var sections []string

	if len(m.history) == 0 && !m.agentBusy && !m.streaming {
		sections = append(sections, m.viewHome())
	} else {
		sections = append(sections, m.viewport.View())
	}

	if live := m.viewLiveRegion(); live != "" {
		sections = append(sections, live)
	}

	if m.lastError != "" {
		sections = append(sections, styleError.Width(m.width-2).Render("error: "+m.lastError))
	}

	if m.compactionBanner != "" {
		sections = append(sections, lipgloss.NewStyle().Foreground(colSubtle).Width(m.width-2).Render(m.compactionBanner))
	}

	sections = append(sections, m.viewStatusBar())

	if m.permPrompt != nil {
		sections = append(sections, m.permPrompt.render(m.width))
	} else if m.permBatch != nil {
		sections = append(sections, m.permBatch.render(m.width))
	} else {
		sections = append(sections, m.viewInput())
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

func (m *Model) viewLiveRegion() string {
	if !m.agentBusy && len(m.activeTools) == 0 {
		return ""
	}

	var b strings.Builder

	for _, idx := range m.toolOrder {
		at, ok := m.activeTools[idx]
		if !ok {
			continue
		}
		row := fmt.Sprintf("%s  %s  %s",
			at.spinner.View(),
			styleToolName.Render(at.name),
			styleToolSummary.Render(at.summary),
		)
		b.WriteString(styleToolRow.Render(row) + "\n")
	}

	if m.streaming && m.streamLines != "" {
		preview := lastLines(m.streamLines, liveRegionMaxLines)
		preview = softenAssistantParagraphBreaks(preview)
		innerW := m.width - 10
		if innerW < 24 {
			innerW = 24
		}
		b.WriteString(lipgloss.NewStyle().Width(innerW).Render(preview))
	}

	content := b.String()
	if content == "" {
		return ""
	}
	return styleLiveRegion.Width(m.width - 4).Render(strings.TrimRight(content, "\n"))
}

func (m *Model) viewStatusBar() string {
	w := m.width

	left := styleStatusModel.Render(" " + m.modelName + " ")

	tokenStr := fmt.Sprintf(" in:%s out:%s ",
		formatTokens(m.totalInputTokens),
		formatTokens(m.totalOutputTokens),
	)
	right := styleStatusTokens.Render(tokenStr)

	centre := ""
	if m.agentBusy {
		centre = styleStatusBar.Render(" ● ")
	}

	usedWidth := lipgloss.Width(left) + lipgloss.Width(centre) + lipgloss.Width(right)
	gap := w - usedWidth
	if gap < 0 {
		gap = 0
	}
	fill := styleStatusBar.Width(gap).Render("")

	return lipgloss.JoinHorizontal(lipgloss.Top,
		left,
		fill,
		centre,
		right,
	)
}

func (m *Model) viewInput() string {
	var border lipgloss.Style
	if m.inputFocused {
		border = styleInputBorderFocused
	} else {
		border = styleInputBorder
	}

	input := border.Width(m.width - 2).Render(m.textarea.View())

	if m.showAuto {
		auto := m.viewAutoComplete()
		if auto != "" {
			return lipgloss.JoinVertical(lipgloss.Left, auto, input)
		}
	}
	return input
}

func (m *Model) viewAutoComplete() string {
	items := m.filteredAuto()
	if len(items) == 0 {
		return ""
	}

	if len(items) > 6 {
		items = items[:6]
	}

	var rows []string
	for i, item := range items {
		label := "/" + item.name
		desc := item.desc
		var row string
		if i == m.autoIndex {
			row = styleAutoItemSelected.Render(
				fmt.Sprintf("%-16s %s", label, desc),
			)
		} else {
			row = styleAutoItem.Render(
				fmt.Sprintf("%-16s %s", label, desc),
			)
		}
		rows = append(rows, row)
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colBorder).
		Width(m.width - 4).
		Render(strings.Join(rows, "\n"))

	return box
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

