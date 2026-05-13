// internal/commands/init.go
package commands

import (
	"fmt"
	"os"
	"path/filepath"
)

const commandTemplate = `---
name: %s
description: %s
agent: %s
model: anthropic/claude-sonnet-4
risk_tier: %d
---

%s
`

var starterCommands = map[string]struct {
	description string
	agent       string
	riskTier    int
	template    string
}{
	"implement": {
		description: "End-to-end ticket implementation (Research → Plan → Execute → Review → Commit)",
		agent:       "orchestrator",
		riskTier:    2,
		template:    "Implement ticket {ticket_id} following the full RPI workflow.\n\nCurrent stage: {stage|full}\n\nAlways run review_my_changes before committing.",
	},
	"review": {
		description: "Review recent changes or a specific ticket",
		agent:       "reviewer",
		riskTier:    1,
		template:    "Review the following changes:\n\n!`git diff --cached`\n\nProvide feedback on code quality, security, and architecture.",
	},
	"plan": {
		description: "Create or update a detailed implementation plan",
		agent:       "planner",
		riskTier:    1,
		template:    "Create a detailed implementation plan for ticket {ticket_id}.\nOutput the plan to PLAN.md using the write_plan tool.",
	},
	"security-audit": {
		description: "Perform a focused security review",
		agent:       "reviewer",
		riskTier:    3,
		template:    "Perform a security audit on recent changes.\nCheck for injection vulnerabilities, auth issues, secret leakage, insecure dependencies, etc.",
	},
	"refactor": {
		description: "Refactor and improve code quality",
		agent:       "executor",
		riskTier:    2,
		template:    "Review and refactor the following code for cleanliness and maintainability:\n\n@{{file_path}}",
	},
	"database-migration": {
		description: "Create or review a database migration",
		agent:       "executor",
		riskTier:    3,
		template:    "Create/review a database migration for ticket {ticket_id}.\nInclude up/down migrations, indexes, and tests.",
	},
	"hotfix": {
		description: "Create a production hotfix",
		agent:       "executor",
		riskTier:    3,
		template:    "Create a minimal, safe hotfix for issue {ticket_id}.\nPrioritize stability and include regression tests.",
	},
	"onboard": {
		description: "Project onboarding summary",
		agent:       "researcher",
		riskTier:    1,
		template:    "Provide a project onboarding summary.\nRead README.md, .drover.md, AGENTS.md and summarize structure and standards.",
	},
	"release-notes": {
		description: "Generate release notes from recent commits",
		agent:       "reviewer",
		riskTier:    1,
		template:    "Generate professional release notes from recent commits:\n\n!`git log --oneline -20`",
	},
}

// Init creates the .drover/commands directory and populates it with useful starter commands
func Init(workDir string) error {
	cmdDir := filepath.Join(workDir, ".drover", "commands")

	if err := os.MkdirAll(cmdDir, 0755); err != nil {
		return fmt.Errorf("failed to create commands directory: %w", err)
	}

	created := 0
	for name, def := range starterCommands {
		filePath := filepath.Join(cmdDir, name+".md")

		// Skip if file already exists
		if _, err := os.Stat(filePath); err == nil {
			fmt.Printf("⚠️  %s.md already exists, skipping...\n", name)
			continue
		}

		content := fmt.Sprintf(commandTemplate,
			name,
			def.description,
			def.agent,
			def.riskTier,
			def.template,
		)

		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s.md: %w", name, err)
		}

		fmt.Printf("✅ Created .drover/commands/%s.md\n", name)
		created++
	}

	if created == 0 {
		fmt.Println("✅ All default commands already exist.")
	} else {
		fmt.Printf("\n🎉 Successfully created %d starter custom commands!\n", created)
		fmt.Println("\nYou can now use commands like:")
		fmt.Println("  /implement PAP-1234")
		fmt.Println("  /review")
		fmt.Println("  /security-audit")
		fmt.Println("\nCustomize them anytime in .drover/commands/")
	}

	return nil
}
