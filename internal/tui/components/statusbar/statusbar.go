package statusbar

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cloudshuttle/drover-code/internal/tui/styles"
)

// StatusBar shows the current model, token usage, and busy state.
// This is one of the first components we extract in the Week 1 plan.
type StatusBar struct {
	ModelName    string
	InputTokens  int
	OutputTokens int
	AgentBusy    bool
	Width        int

	RiskLevel  string // "normal", "caution", "high"
	RiskReason string // short explanation (e.g. "editing source files")
}

// New creates a new StatusBar.
func New(modelName string) *StatusBar {
	return &StatusBar{
		ModelName: modelName,
	}
}

func (s *StatusBar) SetSize(width, _ int) {
	s.Width = width
}

// Update can be expanded later for dynamic updates.
func (s *StatusBar) Update(msg tea.Msg) (*StatusBar, tea.Cmd) {
	return s, nil
}

func (s *StatusBar) View() string {
	if s.Width == 0 {
		return ""
	}

	left := lipgloss.NewStyle().Foreground(styles.ColAccent).Bold(true).Render("◉ " + s.ModelName)

	busy := ""
	if s.AgentBusy {
		busy = lipgloss.NewStyle().Foreground(styles.ColSuccess).Render(" ● LIVE")
	}

	risk := s.renderRisk()

	right := fmt.Sprintf("%s  in:%d  out:%d%s",
		busy,
		s.InputTokens,
		s.OutputTokens,
		risk,
	)

	used := lipgloss.Width(left) + lipgloss.Width(right)
	fillWidth := s.Width - used
	if fillWidth < 0 {
		fillWidth = 0
	}

	filler := lipgloss.NewStyle().
		Width(fillWidth).
		Background(styles.ColSurface).
		Render(" ")

	return lipgloss.JoinHorizontal(lipgloss.Top, left, filler, right)
}

func (s *StatusBar) renderRisk() string {
	level := strings.ToLower(strings.TrimSpace(s.RiskLevel))
	if level == "" || level == "normal" {
		return ""
	}

	var indicator string
	switch level {
	case "high", "danger", "critical":
		indicator = lipgloss.NewStyle().Foreground(styles.ColError).Bold(true).Render(" ● HIGH")
	case "caution", "warning", "medium":
		indicator = lipgloss.NewStyle().Foreground(styles.ColWarning).Bold(true).Render(" ● CAUTION")
	default:
		indicator = lipgloss.NewStyle().Foreground(styles.ColMuted).Render(" ● " + strings.ToUpper(level))
	}

	label := "Guard:" + indicator
	if s.RiskReason != "" {
		short := s.RiskReason
		if len(short) > 28 {
			short = short[:25] + "…"
		}
		label += " " + lipgloss.NewStyle().Foreground(styles.ColMuted).Render("("+short+")")
	}
	return "  " + label
}