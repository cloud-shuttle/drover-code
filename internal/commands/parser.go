package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ParseMarkdownCommand parses a markdown file with simple frontmatter format.
func ParseMarkdownCommand(path string) (CommandDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CommandDefinition{}, err
	}

	content := string(data)

	// Extract frontmatter
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return CommandDefinition{}, fmt.Errorf("invalid frontmatter in %s", path)
	}

	frontmatter := strings.TrimSpace(parts[1])
	template := strings.TrimSpace(parts[2])

	cmd := CommandDefinition{
		Template: template,
		Source:   "file:" + path,
		LoadedAt: time.Now(),
		FilePath: path,
	}

	lines := strings.Split(frontmatter, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])

		// strip inline comments naively
		if idx := strings.Index(val, " #"); idx != -1 {
			val = strings.TrimSpace(val[:idx])
		}

		switch key {
		case "name":
			cmd.Name = val
		case "description":
			cmd.Description = val
		case "agent":
			cmd.Agent = val
		case "model":
			cmd.Model = val
		case "risk_tier":
			if v, err := strconv.Atoi(val); err == nil {
				cmd.RiskTier = v
			}
		case "subtask":
			if val == "true" {
				cmd.Subtask = true
			}
		}
	}

	// Use filename as fallback name
	if cmd.Name == "" {
		cmd.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	return cmd, nil
}

// ParseJSONConfig parses a JSON configuration file containing commands.
func ParseJSONConfig(path string) (map[string]CommandDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config struct {
		Commands map[string]CommandDefinition `json:"commands"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	for name, cmd := range config.Commands {
		cmd.Name = name
		cmd.Source = "config:" + path
		cmd.LoadedAt = time.Now()
		config.Commands[name] = cmd
	}

	return config.Commands, nil
}
