package config

import (
	"encoding/json"
	"strings"
)

// UnmarshalJSON accepts "theme" as a string or as an object.
// A string is the Claude Code form, for example "dark".
func (s *Settings) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	theme, hasTheme := raw["theme"]
	delete(raw, "theme")
	rest, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	type plain Settings
	var base plain
	if err := json.Unmarshal(rest, &base); err != nil {
		return err
	}
	*s = Settings(base)
	if !hasTheme || len(theme) == 0 || string(theme) == "null" {
		return nil
	}
	if theme[0] == '"' {
		var name string
		if err := json.Unmarshal(theme, &name); err != nil {
			return err
		}
		applyThemeName(&s.Theme, name)
		return nil
	}
	return json.Unmarshal(theme, &s.Theme)
}

func applyThemeName(theme *ThemeConfig, name string) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto":
		theme.Mode = "auto"
	case "dark", "dark-mode", "dark-daltonized", "dark-ansi":
		theme.Mode = "dark"
	case "light", "light-mode", "light-daltonized", "light-ansi":
		theme.Mode = "light"
	default:
		lower := strings.ToLower(strings.TrimSpace(name))
		if strings.HasPrefix(lower, "dark") {
			theme.Mode = "dark"
			return
		}
		if strings.HasPrefix(lower, "light") {
			theme.Mode = "light"
			return
		}
		theme.Palette = strings.TrimSpace(name)
	}
}
