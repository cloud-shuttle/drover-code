package commands

import "time"

// CommandDefinition represents a custom command loaded from markdown or config.
type CommandDefinition struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Template    string `json:"template" yaml:"template"`

	// Execution options
	Agent    string `json:"agent,omitempty" yaml:"agent,omitempty"`
	Model    string `json:"model,omitempty" yaml:"model,omitempty"`
	RiskTier int    `json:"risk_tier,omitempty" yaml:"risk_tier,omitempty"`
	Subtask  bool   `json:"subtask,omitempty" yaml:"subtask,omitempty"`

	// Metadata
	Source   string    `json:"source"` // "file:.drover/commands/xxx.md" or "config"
	LoadedAt time.Time `json:"loaded_at"`
	FilePath string    `json:"file_path,omitempty"`
}

// CommandRegistry stores loaded custom commands.
type CommandRegistry struct {
	commands map[string]CommandDefinition
}

// NewRegistry creates a new empty registry.
func NewRegistry() *CommandRegistry {
	return &CommandRegistry{
		commands: make(map[string]CommandDefinition),
	}
}

// Register adds or replaces a command in the registry.
func (r *CommandRegistry) Register(cmd CommandDefinition) {
	r.commands[cmd.Name] = cmd
}

// Get retrieves a command by name.
func (r *CommandRegistry) Get(name string) (CommandDefinition, bool) {
	cmd, ok := r.commands[name]
	return cmd, ok
}

// List returns all registered commands.
func (r *CommandRegistry) List() []CommandDefinition {
	cmds := make([]CommandDefinition, 0, len(r.commands))
	for _, cmd := range r.commands {
		cmds = append(cmds, cmd)
	}
	return cmds
}
