package styles

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ThemeMode controls explicit light/dark forcing (for the Theme System roadmap item).
type ThemeMode string

const (
	ThemeAuto  ThemeMode = "auto"
	ThemeLight ThemeMode = "light"
	ThemeDark  ThemeMode = "dark"
)

// CurrentThemeMode is the resolved theme mode (auto/light/dark).
// It can be influenced by DROVER_CODE_THEME env var.
var CurrentThemeMode = resolveThemeMode()

func resolveThemeMode() ThemeMode {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("DROVER_CODE_THEME")))
	switch v {
	case "light", "light-mode":
		return ThemeLight
	case "dark", "dark-mode":
		return ThemeDark
	default:
		return ThemeAuto
	}
}

// isDark returns whether we should use dark-mode color values.
func isDark() bool {
	if CurrentThemeMode == ThemeLight {
		return false
	}
	if CurrentThemeMode == ThemeDark {
		return true
	}
	// Auto: fall back to lipgloss terminal detection
	return lipgloss.HasDarkBackground()
}

// Colors centralizes all color definitions used across the TUI and its components.
// This is the foundation for the Theme System (Dark/Light + Custom) roadmap item.
//
// Components and the main tui package should import this package and use the exported
// Col* variables instead of redefining the same hex values.
//
// Future: These will be driven by full Theme structs for custom palettes.
var (
	ColBase    = adaptive("#1a1a1a", "#e8e8e8")
	ColMuted   = adaptive("#6b6b6b", "#888888")
	ColSubtle  = adaptive("#9a9a9a", "#555555")
	ColSurface = adaptive("#f4f4f4", "#1e1e1e")
	ColBorder  = adaptive("#d0d0d0", "#333333")

	ColAccent    = adaptive("#b5690a", "#e8a020")
	ColAccentDim = adaptive("#c98020", "#a06010")

	ColSuccess = adaptive("#2d7a2d", "#4caf50")
	ColError   = adaptive("#c0392b", "#ef5350")
	ColWarning = adaptive("#b5690a", "#ffa726")

	ColUserBg = adaptive("#eaf0fb", "#1a2233")
	ColUserFg = adaptive("#1a1a1a", "#dce8ff")
)

// adaptive returns a lipgloss.Color that respects the current ThemeMode.
func adaptive(light, dark string) lipgloss.TerminalColor {
	if isDark() {
		return lipgloss.Color(dark)
	}
	return lipgloss.Color(light)
}

// SetThemeMode allows runtime override (for /theme command, palette picker,
// or DROVER_CODE_THEME). It fully re-resolves the current named palette under
// the new mode and re-applies any custom Colors overrides + notifies listeners
// so the change is immediately visible across all TUI surfaces.
func SetThemeMode(mode ThemeMode) {
	CurrentThemeMode = mode
	// Re-resolve + apply base for the active named theme + overrides.
	if CurrentTheme.Name == "" {
		CurrentTheme.Name = "default"
	}
	ApplyTheme(Theme{Name: CurrentTheme.Name, Mode: mode})
}
