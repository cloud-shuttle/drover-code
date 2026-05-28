package toolspinner

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	"github.com/cloudshuttle/drover-code/internal/tui/styles"
)

// ToolSpinner is a small reusable component for showing an active tool call
// with a spinner, name, and summary.
type ToolSpinner struct {
	Spinner spinner.Model
	Name    string
	Summary string
}

// New creates a new ToolSpinner with the default dot spinner.
func New(name, summary string) *ToolSpinner {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(styles.ColAccentDim) // matching old styleToolPending
	return &ToolSpinner{
		Spinner: s,
		Name:    name,
		Summary: summary,
	}
}

func (t *ToolSpinner) View() string {
	nameStyle := lipgloss.NewStyle().Foreground(styles.ColAccent).Bold(true)
	summaryStyle := lipgloss.NewStyle().Foreground(styles.ColMuted)
	return t.Spinner.View() + " " + nameStyle.Render(t.Name) + " " + summaryStyle.Render(t.Summary)
}