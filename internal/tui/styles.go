package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/cloudshuttle/drover-code/internal/tui/styles"
)

// Re-export central colors for use inside the tui package (maintains existing lowercase names)
var (
	colBase    = styles.ColBase
	colMuted   = styles.ColMuted
	colSubtle  = styles.ColSubtle
	colSurface = styles.ColSurface
	colBorder  = styles.ColBorder

	colAccent    = styles.ColAccent
	colAccentDim = styles.ColAccentDim

	colSuccess = styles.ColSuccess
	colError   = styles.ColError
	colWarning = styles.ColWarning

	colUserBg = styles.ColUserBg
	colUserFg = styles.ColUserFg
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

	StyleStatusBar = lipgloss.NewStyle().
			Foreground(colMuted).
			Background(colSurface)

	StyleStatusModel = lipgloss.NewStyle().
				Foreground(colAccent).
				Background(colSurface).
				Bold(true)

	StyleStatusTokens = lipgloss.NewStyle().
				Foreground(colSubtle).
				Background(colSurface)

	styleInputBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colBorder)

	styleInputBorderFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colAccent)

	// StylePerm* removed (dead code after permissionprompt component extraction + render deletion in permission.go)

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

