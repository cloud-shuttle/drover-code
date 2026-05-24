---
name: sqlforge-apply
description: Apply sqlforge plan after user confirmation (mutates warehouse)
risk_tier: 2
product: platform
audience: platform-operator
doc_type: reference
topics:
  - governance-policy
surface: repo-docs
---
The user has confirmed they want to deploy SQLForge changes to environment **$1**.

1. `cd` to the directory that contains `sqlforge.yml`.
2. Run `sqlforge plan $1` first; if the diff is empty, say so and stop.
3. Run `sqlforge apply $1` (non-TTY apply skips TUI).
4. Report success or errors; suggest `sqlforge snapshot $1` if historized models changed.

Never run apply without this command or an explicit user message to deploy.
