package tui

import "github.com/charmbracelet/lipgloss"

var (
	colBase    = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e8e8e8"}
	colMuted   = lipgloss.AdaptiveColor{Light: "#6b6b6b", Dark: "#888888"}
	colSubtle  = lipgloss.AdaptiveColor{Light: "#9a9a9a", Dark: "#555555"}
	colSurface = lipgloss.AdaptiveColor{Light: "#f4f4f4", Dark: "#1e1e1e"}
	colBorder  = lipgloss.AdaptiveColor{Light: "#d0d0d0", Dark: "#333333"}

	colAccent    = lipgloss.AdaptiveColor{Light: "#b5690a", Dark: "#e8a020"}
	colAccentDim = lipgloss.AdaptiveColor{Light: "#c98020", Dark: "#a06010"}

	colSuccess = lipgloss.AdaptiveColor{Light: "#2d7a2d", Dark: "#4caf50"}
	colError   = lipgloss.AdaptiveColor{Light: "#c0392b", Dark: "#ef5350"}
	colWarning = lipgloss.AdaptiveColor{Light: "#b5690a", Dark: "#ffa726"}

	colUserBg = lipgloss.AdaptiveColor{Light: "#eaf0fb", Dark: "#1a2233"}
	colUserFg = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dce8ff"}
)

const (
	statusBarHeight    = 1
	inputMinHeight     = 3
	inputBorderHeight  = 2
	inputTotalHeight   = inputMinHeight + inputBorderHeight
	permPromptHeight   = 9
	liveRegionMaxLines = 12
)

var (
	styleApp = lipgloss.NewStyle()

	styleUserLabel = lipgloss.NewStyle().
			Foreground(colAccent).
			Bold(true)

	styleUserBubble = lipgloss.NewStyle().
			Background(colUserBg).
			Foreground(colUserFg).
			PaddingLeft(2).
			PaddingRight(2).
			PaddingTop(1).
			PaddingBottom(1).
			MarginBottom(1)

	styleAssistantLabel = lipgloss.NewStyle().
				Foreground(colMuted)

	styleAssistantBody = lipgloss.NewStyle().
				MarginBottom(1)

	styleToolRow = lipgloss.NewStyle().
			PaddingLeft(2)

	styleToolName = lipgloss.NewStyle().
			Foreground(colAccent).
			Bold(true)

	styleToolSummary = lipgloss.NewStyle().
				Foreground(colMuted)

	styleToolDone = lipgloss.NewStyle().
			Foreground(colSuccess)

	styleToolError = lipgloss.NewStyle().
			Foreground(colError)

	styleToolPending = lipgloss.NewStyle().
				Foreground(colAccentDim)

	styleLiveRegion = lipgloss.NewStyle().
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(colAccentDim).
			PaddingLeft(1).
			PaddingTop(1).
			PaddingBottom(1).
			MarginBottom(1)

	styleStatusBar = lipgloss.NewStyle().
			Foreground(colMuted).
			Background(colSurface)

	styleStatusModel = lipgloss.NewStyle().
				Foreground(colAccent).
				Background(colSurface).
				Bold(true)

	styleStatusTokens = lipgloss.NewStyle().
				Foreground(colSubtle).
				Background(colSurface)

	styleInputBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colBorder)

	styleInputBorderFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colAccent)

	stylePermBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colWarning).
			Padding(1, 2)

	stylePermTitle = lipgloss.NewStyle().
			Foreground(colWarning).
			Bold(true)

	stylePermTool = lipgloss.NewStyle().
			Foreground(colAccent).
			Bold(true)

	stylePermSummary = lipgloss.NewStyle().
				Foreground(colMuted)

	stylePermKey = lipgloss.NewStyle().
			Foreground(colBase).
			Background(colSurface).
			Bold(true).
			PaddingLeft(1).
			PaddingRight(1)

	stylePermKeyLabel = lipgloss.NewStyle().
				Foreground(colMuted)

	styleAutoItem = lipgloss.NewStyle().
			PaddingLeft(2).
			Foreground(colMuted)

	styleAutoItemSelected = lipgloss.NewStyle().
				PaddingLeft(2).
				Foreground(colAccent).
				Bold(true)

	styleError = lipgloss.NewStyle().
			Foreground(colError).
			Border(lipgloss.NormalBorder()).
			BorderForeground(colError).
			Padding(0, 1).
			MarginBottom(1)

	styleDivider = lipgloss.NewStyle().
			Foreground(colBorder)

	styleHomeBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colBorder).
			Padding(1, 2)

	styleHomeTitle = lipgloss.NewStyle().
			Foreground(colMuted).
			Bold(true)

	styleHomeWelcome = lipgloss.NewStyle().
			Foreground(colBase).
			Bold(true)

	styleHomeMeta = lipgloss.NewStyle().
			Foreground(colMuted)

	styleHomeTipsHeader = lipgloss.NewStyle().
				Foreground(colMuted).
				Bold(true)

	styleHomeTipsBody = lipgloss.NewStyle().
			Foreground(colMuted)

	styleHomeBrand = lipgloss.NewStyle().
			Foreground(colAccent)
)

func divider(width int) string {
	if width <= 0 {
		return ""
	}
	return styleDivider.Render(lipgloss.NewStyle().
		Width(width).
		Render("─"))
}

