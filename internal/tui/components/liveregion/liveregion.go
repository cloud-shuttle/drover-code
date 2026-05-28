package liveregion

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cloudshuttle/drover-code/internal/tui/components/toolspinner"
	"github.com/cloudshuttle/drover-code/internal/tui/core"
	"github.com/cloudshuttle/drover-code/internal/tui/styles"
)

// LiveRegion handles the live area showing active tools + streaming assistant output.
// This is one of the highest-value extractions in the Week 1 plan.
type LiveRegion struct {
	Streaming      bool
	StreamLines    string
	ActiveTools    map[int]*toolspinner.ToolSpinner
	ToolOrder      []int
	CompletedTools []core.CompletedTool
	Width          int
}

func New() *LiveRegion {
	return &LiveRegion{
		ActiveTools: make(map[int]*toolspinner.ToolSpinner),
	}
}

func (l *LiveRegion) SetSize(width, _ int) {
	l.Width = width
}

// DrainCompletedTools returns the completed tools collected by this LiveRegion
// and clears them. This allows the caller (Model) to attach them to history
// without needing the legacy pendingDone slice for component-managed tools.
func (l *LiveRegion) DrainCompletedTools() []core.CompletedTool {
	if len(l.CompletedTools) == 0 {
		return nil
	}
	out := l.CompletedTools
	l.CompletedTools = nil
	return out
}

func (l *LiveRegion) View() string {
	if !l.Streaming && len(l.ActiveTools) == 0 {
		return ""
	}

	var b strings.Builder

	// Active tool spinners
	for _, idx := range l.ToolOrder {
		if ts, ok := l.ActiveTools[idx]; ok {
			row := fmt.Sprintf("%s  %s  %s",
				ts.Spinner.View(),
				lipgloss.NewStyle().Foreground(styles.ColAccent).Bold(true).Render(ts.Name),
				lipgloss.NewStyle().Foreground(styles.ColMuted).Render(ts.Summary),
			)
			b.WriteString(lipgloss.NewStyle().PaddingLeft(2).Render(row) + "\n")
		}
	}

	// Streaming preview (raw text, last N lines, with softening)
	if l.Streaming && l.StreamLines != "" {
		preview := lastLines(l.StreamLines, 12)
		preview = softenAssistantParagraphBreaks(preview)

		innerW := l.Width - 10
		if innerW < 24 {
			innerW = 24
		}
		b.WriteString(lipgloss.NewStyle().Width(innerW).Render(preview))
	}

	content := strings.TrimRight(b.String(), "\n")
	if content == "" {
		return ""
	}

	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(styles.ColAccentDim).
		PaddingLeft(1).
		PaddingTop(1).
		PaddingBottom(1).
		Width(l.Width - 4).
		Render(content)
}

// lastLines and softenAssistantParagraphBreaks are copied from the old view logic
// (we can later move them to a shared util if desired).
func lastLines(text string, max int) string {
	lines := strings.Split(text, "\n")
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	return strings.Join(lines, "\n")
}

func softenAssistantParagraphBreaks(text string) string {
	repls := []struct{ from, to string }{
		{":Now ", ":\n\nNow "},
		{":Let me ", ":\n\nLet me "},
		{":The ", ":\n\nThe "},
		{":I ", ":\n\nI "},
		{":We ", ":\n\nWe "},
		{":Here", ":\n\nHere"},
		{":Good!", ":\n\nGood!"},
	}
	out := text
	for _, r := range repls {
		out = strings.ReplaceAll(out, r.from, r.to)
	}
	return out
}