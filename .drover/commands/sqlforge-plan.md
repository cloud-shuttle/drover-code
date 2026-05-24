---
name: sqlforge-plan
description: Run sqlforge plan for an environment and summarize DDL changes (no apply)
risk_tier: 0
product: platform
audience: platform-operator
doc_type: reference
topics:
  - governance-policy
surface: repo-docs
---
Run `sqlforge plan` for environment **$1** (default: value of `default_environment` in `sqlforge.yml`, usually `prod`).

1. `cd` to the directory that contains `sqlforge.yml`.
2. Execute `sqlforge plan $1` via bash.
3. Summarize models added/changed/removed and any impacted downstream models.
4. Do **not** run `sqlforge apply` unless the user explicitly asks in a follow-up.

Use Brain MCP only for repository code questions—not for warehouse tables or metrics.
