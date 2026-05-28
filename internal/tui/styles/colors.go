package styles

import "github.com/charmbracelet/lipgloss"

// Colors centralizes all AdaptiveColor definitions used across the TUI and its components.
// This eliminates duplication that previously existed in each component package.
//
// Components and the main tui package should import this package and use the exported
// Col* variables instead of redefining the same hex values.

var (
	ColBase    = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e8e8e8"}
	ColMuted   = lipgloss.AdaptiveColor{Light: "#6b6b6b", Dark: "#888888"}
	ColSubtle  = lipgloss.AdaptiveColor{Light: "#9a9a9a", Dark: "#555555"}
	ColSurface = lipgloss.AdaptiveColor{Light: "#f4f4f4", Dark: "#1e1e1e"}
	ColBorder  = lipgloss.AdaptiveColor{Light: "#d0d0d0", Dark: "#333333"}

	ColAccent    = lipgloss.AdaptiveColor{Light: "#b5690a", Dark: "#e8a020"}
	ColAccentDim = lipgloss.AdaptiveColor{Light: "#c98020", Dark: "#a06010"}

	ColSuccess = lipgloss.AdaptiveColor{Light: "#2d7a2d", Dark: "#4caf50"}
	ColError   = lipgloss.AdaptiveColor{Light: "#c0392b", Dark: "#ef5350"}
	ColWarning = lipgloss.AdaptiveColor{Light: "#b5690a", Dark: "#ffa726"}

	ColUserBg = lipgloss.AdaptiveColor{Light: "#eaf0fb", Dark: "#1a2233"}
	ColUserFg = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dce8ff"}
)
