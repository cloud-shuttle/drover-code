package commandpalette

// Command represents an entry in the Command Palette.
//
// It supports two modes:
//   - Text commands (default): Selecting it will cause the main TUI to inject
//     "/Name " into the input textarea.
//   - Semantic actions: When Key is set to a known action (e.g. "compact",
//     "clear"), the main Model can execute it directly without text injection.
//
// This allows both backward-compatible slash commands and richer first-class
// actions (Compact Context, Clear Conversation, etc.).
type Command struct {
	Name        string
	Description string

	// ActionKey enables direct semantic execution (see executePaletteAction).
	ActionKey string

	// Optional rich metadata for better UX in the palette
	Category  string // e.g. "Agent", "TUI", "Custom"
	Shortcut  string // e.g. "⌘K C" or "Ctrl+K, C"
	RiskLevel string // "normal", "caution", "high" — for risk-aware coloring/filtering
}

// IsSemantic returns true if this command should trigger a direct action
// instead of text injection.
func (c Command) IsSemantic() bool {
	return c.ActionKey != ""
}
