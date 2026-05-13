# Custom Commands Infrastructure

## Overview

Drover Code now supports a robust **Custom Commands** system, enabling users to create reusable, context-aware command templates (`/implement`, `/review`, `/deploy`, etc.). This feature brings high usability and agentic orchestration capabilities natively to the Go runtime, operating headlessly or via the TUI.

## Command Loading and Priority

Custom commands are loaded in the following order of precedence (highest to lowest):
1. **Project Local:** `.drover/commands/*.md`
2. **Global:** `~/.drover/commands/*.md`
3. **Configuration:** `Settings.Commands` (JSON configuration fallback)

## Markdown Format

Commands defined in Markdown files utilize a flat YAML frontmatter syntax combined with the template content body.

```markdown
---
name: security-audit
description: Runs a thorough security audit on the current workspace.
agent: security-specialist
model: claude-haiku-4-5-20251001
risk_tier: 3
subtask: true
---
Perform a security audit using the following requirements:
@security-policy.md

Focus on the components specified here: $1
```

## Template Expansion

The `TemplateExpander` evaluates command templates prior to invoking the core agentic loop:
- **Positional Arguments:** `$1`, `$2`, etc.
- **Bulk Arguments:** `$ARGUMENTS` (expands to all remaining unparsed arguments)
- **Placeholders:** `{var}` or `{var|default}`
- **File Inclusion:** `@filename.txt` (reads relative to workspace)
- **Shell Commands:** ``!`shell_command` `` (executes locally, 30s timeout)

## Drover Guard Integration

To maintain Drover's Layer 6 Governance standards, all custom commands are evaluated by `Drover Guard` before expansion and execution. 
- A REST API client (`pkg/guardclient`) passes the `TenantID`, `AgentID`, `RiskTier`, and `Action` context to the Guard policy engine.
- If Guard denies the request, execution is blocked immediately and the prompt returns an error.
- Decoupling this layer ensures minimal footprint for `drover-code` while still enforcing strong enterprise safety rails.
