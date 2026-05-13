package commands

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/config"
)

// Loader loads custom commands from file systems and configurations.
type Loader struct {
	workDir  string
	registry *CommandRegistry
}

// NewLoader creates a new Loader instance.
func NewLoader(workDir string) *Loader {
	return &Loader{
		workDir:  workDir,
		registry: NewRegistry(),
	}
}

// LoadAll loads commands from project directory, global directory, and JSON config settings.
func (l *Loader) LoadAll(settings config.Settings) error {
	// 1. Load from project .drover/commands/
	projectDir := filepath.Join(l.workDir, ".drover", "commands")
	if err := l.loadFromDir(projectDir); err != nil {
		return err
	}

	// 2. Load from global ~/.drover/commands/
	if home, err := os.UserHomeDir(); err == nil {
		globalDir := filepath.Join(home, ".drover", "commands")
		_ = l.loadFromDir(globalDir) // ignore errors for global
	}

	// 3. Load from config settings directly.
	for name, cfg := range settings.Commands {
		cmd := CommandDefinition{
			Name:        name,
			Description: cfg.Description,
			Template:    cfg.Template,
			Agent:       cfg.Agent,
			Model:       cfg.Model,
			RiskTier:    cfg.RiskTier,
			Subtask:     cfg.Subtask,
			Source:      "config",
		}
		l.registry.Register(cmd)
	}

	return nil
}

func (l *Loader) loadFromDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		if strings.HasSuffix(path, ".md") {
			cmd, err := ParseMarkdownCommand(path)
			if err == nil {
				l.registry.Register(cmd)
			}
		}
		return nil
	})
}

// GetRegistry returns the underlying registry with loaded commands.
func (l *Loader) GetRegistry() *CommandRegistry {
	return l.registry
}
