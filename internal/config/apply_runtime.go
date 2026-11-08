package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/tui/styles"
)

// ApplyConvoHeuristics sets context limit and chars-per-token on mgr from
// merged settings, with DROVER_CODE_CONTEXT_LIMIT_EST and
// DROVER_CODE_CHARS_PER_TOKEN env overrides (same semantics as the CLI).
func ApplyConvoHeuristics(mgr *convo.Manager, s Settings) {
	n := s.ContextLimitEstimate
	if v := envIntPositive("DROVER_CODE_CONTEXT_LIMIT_EST"); v > 0 {
		n = v
	}
	if n > 0 {
		mgr.SetContextLimit(n)
	}

	c := s.CharsPerTokenEstimate
	if v := envIntPositive("DROVER_CODE_CHARS_PER_TOKEN"); v > 0 {
		c = v
	}
	if c > 0 {
		mgr.SetCharsPerToken(c)
	}
}

// ApplyAgentLoopOptions applies workflow flags from merged settings (e.g. auto-compaction).
func ApplyAgentLoopOptions(loop *agent.Loop, s Settings) {
	loop.ApplyWorkflowSettings(EffectiveDisableAutoCompaction(s))
}

// ApplyTheme applies theme settings from merged config + env overrides.
// This is the wiring point for the TUI Theme System (Dark/Light + Custom).
func ApplyTheme(s Settings) {
	mode := styles.ThemeAuto
	if s.Theme.Mode != "" {
		mode = styles.ThemeMode(strings.ToLower(s.Theme.Mode))
	}
	// Env var still wins for quick overrides (DROVER_CODE_THEME)
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("DROVER_CODE_THEME"))); v != "" {
		mode = styles.ThemeMode(v)
	}

	paletteName := "default"
	if s.Theme.Palette != "" {
		paletteName = s.Theme.Palette
	}

	styles.SetNamedPalette(paletteName, mode)

	// Fully apply custom color overrides from settings (Theme System)
	if len(s.Theme.Colors) > 0 {
		styles.ApplyColorOverrides(s.Theme.Colors)
	}
}

func envIntPositive(key string) int {
	s := strings.TrimSpace(os.Getenv(key))
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
