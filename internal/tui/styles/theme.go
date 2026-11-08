package styles

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Palette defines a set of colors for a specific theme variant.
type Palette struct {
	Base       lipgloss.TerminalColor
	Muted      lipgloss.TerminalColor
	Subtle     lipgloss.TerminalColor
	Surface    lipgloss.TerminalColor
	Border     lipgloss.TerminalColor
	Accent     lipgloss.TerminalColor
	AccentDim  lipgloss.TerminalColor
	Success    lipgloss.TerminalColor
	Error      lipgloss.TerminalColor
	Warning    lipgloss.TerminalColor
	UserBg     lipgloss.TerminalColor
	UserFg     lipgloss.TerminalColor
}

// Theme represents a complete visual theme for the TUI.
type Theme struct {
	Name    string
	Mode    ThemeMode
	Palette Palette
}

// Built-in named palettes. These are the foundation for swappable themes.
var (
	// DefaultDark is the current dark-mode colors.
	DefaultDark = Palette{
		Base:      lipgloss.Color("#e8e8e8"),
		Muted:     lipgloss.Color("#888888"),
		Subtle:    lipgloss.Color("#555555"),
		Surface:   lipgloss.Color("#1e1e1e"),
		Border:    lipgloss.Color("#333333"),
		Accent:    lipgloss.Color("#e8a020"),
		AccentDim: lipgloss.Color("#a06010"),
		Success:   lipgloss.Color("#4caf50"),
		Error:     lipgloss.Color("#ef5350"),
		Warning:   lipgloss.Color("#ffa726"),
		UserBg:    lipgloss.Color("#1a2233"),
		UserFg:    lipgloss.Color("#dce8ff"),
	}

	// DefaultLight is the current light-mode colors.
	DefaultLight = Palette{
		Base:      lipgloss.Color("#1a1a1a"),
		Muted:     lipgloss.Color("#6b6b6b"),
		Subtle:    lipgloss.Color("#9a9a9a"),
		Surface:   lipgloss.Color("#f4f4f4"),
		Border:    lipgloss.Color("#d0d0d0"),
		Accent:    lipgloss.Color("#b5690a"),
		AccentDim: lipgloss.Color("#c98020"),
		Success:   lipgloss.Color("#2d7a2d"),
		Error:     lipgloss.Color("#c0392b"),
		Warning:   lipgloss.Color("#b5690a"),
		UserBg:    lipgloss.Color("#eaf0fb"),
		UserFg:    lipgloss.Color("#1a1a1a"),
	}

	// HighContrast is an example of a swappable named palette (high visibility).
	HighContrast = Palette{
		Base:      lipgloss.Color("#ffffff"),
		Muted:     lipgloss.Color("#aaaaaa"),
		Subtle:    lipgloss.Color("#888888"),
		Surface:   lipgloss.Color("#000000"),
		Border:    lipgloss.Color("#ffffff"),
		Accent:    lipgloss.Color("#ffff00"),
		AccentDim: lipgloss.Color("#ffaa00"),
		Success:   lipgloss.Color("#00ff00"),
		Error:     lipgloss.Color("#ff0000"),
		Warning:   lipgloss.Color("#ffaa00"),
		UserBg:    lipgloss.Color("#003366"),
		UserFg:    lipgloss.Color("#ffffff"),
	}
)

// NamedPalettes is the registry of built-in swappable palettes.
var NamedPalettes = map[string]struct {
	Light Palette
	Dark  Palette
}{
	"default": {Light: DefaultLight, Dark: DefaultDark},
	"high-contrast": {Light: HighContrast, Dark: HighContrast},
}

// CurrentTheme holds the active theme. Start with a sensible default.
var CurrentTheme = Theme{
	Name: "default",
	Mode: ThemeAuto,
	Palette: DefaultDark, // will be resolved on first use
}

// activeColorOverrides stores the last-applied custom overrides from
// Settings.Theme.Colors so they can be re-applied after any palette/mode
// switch at runtime (ensuring custom colors win over the base named palette).
var activeColorOverrides map[string]string

// themeChangeListeners are notified after any color-affecting change so that
// packages holding pre-captured lipgloss styles can rebuild them for live
// theme/override hot-reload (used by historyview, inputarea autocomplete, etc).
var themeChangeListeners []func()

// ResolvePalette returns the appropriate palette for the current theme + mode.
func ResolvePalette() Palette {
	entry, ok := NamedPalettes[CurrentTheme.Name]
	if !ok {
		entry = NamedPalettes["default"]
	}

	if CurrentTheme.Mode == ThemeLight || (CurrentTheme.Mode == ThemeAuto && !isDark()) {
		return entry.Light
	}
	return entry.Dark
}

// ApplyTheme sets the current theme and updates the global Col* variables.
// After setting the base palette it re-applies any active custom Color overrides
// (from Settings.Theme.Colors) so they always win, then notifies listeners for
// live hot-reload of pre-built component styles.
func ApplyTheme(t Theme) {
	CurrentTheme = t
	p := ResolvePalette()

	ColBase = p.Base
	ColMuted = p.Muted
	ColSubtle = p.Subtle
	ColSurface = p.Surface
	ColBorder = p.Border
	ColAccent = p.Accent
	ColAccentDim = p.AccentDim
	ColSuccess = p.Success
	ColError = p.Error
	ColWarning = p.Warning
	ColUserBg = p.UserBg
	ColUserFg = p.UserFg

	applyOverridesToCurrent()
	NotifyThemeChanged()
}

// SetNamedPalette switches to one of the built-in named palettes and applies it.
func SetNamedPalette(name string, mode ThemeMode) {
	if _, ok := NamedPalettes[name]; !ok {
		name = "default"
	}
	t := Theme{Name: name, Mode: mode}
	ApplyTheme(t)
}

// RegisterThemeChangeListener registers a callback to be invoked whenever the
// active theme, named palette, or custom Colors overrides change at runtime.
// Components that pre-build lipgloss styles at init time should register a
// rebuild func here so that Settings.Theme.Colors (and palette switches) take
// full visual effect without requiring a restart.
func RegisterThemeChangeListener(fn func()) {
	if fn != nil {
		themeChangeListeners = append(themeChangeListeners, fn)
	}
}

// NotifyThemeChanged is called after color mutations; it invokes all registered
// listeners so static styles can be rebuilt for live updates.
func NotifyThemeChanged() {
	for _, fn := range themeChangeListeners {
		if fn != nil {
			fn()
		}
	}
}

// ApplyColorOverrides takes a map of color name -> hex value and overrides
// the current active colors at runtime. This enables custom user palettes
// from settings.json (e.g. "theme.colors.accent": "#ff00ff").
//
// It stores the overrides so they survive future named-palette or mode
// switches (fully applying custom Colors on top of any base at runtime).
func ApplyColorOverrides(overrides map[string]string) {
	if len(overrides) == 0 {
		return
	}

	if activeColorOverrides == nil {
		activeColorOverrides = make(map[string]string)
	}
	for name, hex := range overrides {
		key := strings.ToLower(strings.TrimSpace(name))
		activeColorOverrides[key] = hex
	}

	applyOverridesToCurrent()
	NotifyThemeChanged()
}

// applyOverridesToCurrent mutates the Col* vars from the stored active map.
// Called after every base palette application and from ApplyColorOverrides.
func applyOverridesToCurrent() {
	if len(activeColorOverrides) == 0 {
		return
	}

	// Map of known color names to their setters
	setters := map[string]*lipgloss.TerminalColor{
		"base":       &ColBase,
		"muted":      &ColMuted,
		"subtle":     &ColSubtle,
		"surface":    &ColSurface,
		"border":     &ColBorder,
		"accent":     &ColAccent,
		"accentdim":  &ColAccentDim,
		"success":    &ColSuccess,
		"error":      &ColError,
		"warning":    &ColWarning,
		"userbg":     &ColUserBg,
		"userfg":     &ColUserFg,
	}

	for name, hex := range activeColorOverrides {
		if target, ok := setters[name]; ok {
			*target = lipgloss.Color(hex)
		}
	}
}
