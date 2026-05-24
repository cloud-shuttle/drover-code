package sqlforge

import (
	"fmt"
	"strings"
)

// SystemPrompt returns agent guidance when a data project is present (CLI invocation path).
func SystemPrompt(p Project) string {
	env := p.DefaultEnvironment
	if env == "" {
		env = "prod"
	}

	var b strings.Builder
	b.WriteString("\n\n## Drover SQLForge (data project detected)\n\n")
	fmt.Fprintf(&b, "This workspace includes [`sqlforge.yml`](%s) at `%s`. Use **Drover SQLForge** for warehouse **models**, **metrics**, and **plan**/**apply**—not **Drover Brain** MCP for table or metric questions.\n\n",
		p.ManifestPath, p.Root)
	b.WriteString("| Task | CLI (run from project root) |\n|------|-----------------------------|\n")
	b.WriteString("| Diff | `sqlforge plan ")
	fmt.Fprintf(&b, "%s`\n", env)
	b.WriteString("| Deploy | `sqlforge apply ")
	fmt.Fprintf(&b, "%s` — mutates warehouse; confirm with the user first |\n", env)
	b.WriteString("| Metric SQL | `sqlforge query <metric> ")
	fmt.Fprintf(&b, "%s`\n", env)
	b.WriteString("| SCD snapshots | `sqlforge snapshot ")
	fmt.Fprintf(&b, "%s`\n", env)
	b.WriteString("| Column lineage | `sqlforge lineage <model>`\n")
	b.WriteString("| New environment | `sqlforge env create <name> --base-env ")
	fmt.Fprintf(&b, "%s`\n|\n", env)
	b.WriteString("\n**Rules:**\n")
	b.WriteString("- Run `sqlforge` via `bash` from the project root above.\n")
	b.WriteString("- After editing `models/` or `snapshots/`, run **plan** before **apply**.\n")
	b.WriteString("- Prefer **metric query** for analytics; do not invent warehouse DDL outside SQLForge.\n")
	b.WriteString("- **Brain MCP** = codebase knowledge; **SQLForge** = data plane (monorepo ADR: `drover-sqlforge/docs/adr/0002-cli-invocation-drover-code-integration.md`).\n")
	b.WriteString("- Optional: sidecar `sqlforge mcp ")
	fmt.Fprintf(&b, "%s` for `list_metrics` / `query_metric` / `plan_change` → `apply_change`.\n", env)
	return b.String()
}
