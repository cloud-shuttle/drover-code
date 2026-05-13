// internal/commands/list.go
package commands

import (
	"fmt"
	"sort"
	"strings"
)

// List prints all available custom commands in a nice format
func List(registry *CommandRegistry) {
	cmds := registry.List()
	if len(cmds) == 0 {
		fmt.Println("No custom commands found.")
		fmt.Println("Run 'drover-code commands init' to create starter commands.")
		return
	}

	fmt.Println("Available Custom Commands")
	fmt.Println("=========================")
	fmt.Println()

	// Sort by name for consistent output
	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i].Name < cmds[j].Name
	})

	for _, cmd := range cmds {
		name := "/" + cmd.Name
		desc := cmd.Description
		if desc == "" {
			desc = "(no description)"
		}

		// Add extra info if available
		extra := []string{}
		if cmd.Agent != "" && cmd.Agent != "default" {
			extra = append(extra, "agent:"+cmd.Agent)
		}
		if cmd.RiskTier > 0 {
			extra = append(extra, fmt.Sprintf("risk:%d", cmd.RiskTier))
		}
		if cmd.Subtask {
			extra = append(extra, "subtask")
		}

		extraStr := ""
		if len(extra) > 0 {
			extraStr = "  [" + strings.Join(extra, ", ") + "]"
		}

		fmt.Printf("%-18s → %s%s\n", name, desc, extraStr)
	}

	fmt.Println()
	fmt.Println("Usage: Type /command-name in the TUI")
	fmt.Println("Tip: Run 'drover-code commands init' to add more starter commands")
}
