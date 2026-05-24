---
title: SQLForge from Drover Code
description: Run sqlforge plan and apply from an agent workspace for warehouse model changes.
product: drover-code
audience:
  - platform-operator
  - agent
doc_type: how-to
topics:
  - data-warehousing
  - agent-jobs
surface: repo-docs
---

# SQLForge from Drover Code

When the agent workspace is a **Drover SQLForge** data project (`sqlforge.yml` at the project root), Drover Code automatically injects CLI guidance into the system prompt. Agents use the **`sqlforge` binary** via `bash`—the same path as a local data engineer ([ADR 0002](../../../drover-sqlforge/docs/adr/0002-cli-invocation-drover-code-integration.md)).

## Detection

On `Load()`, the config loader walks upward from `workDir` for `sqlforge.yml` and appends a **Drover SQLForge** section to `SystemInjection()` (`internal/integrations/sqlforge`).

Requirements:

- `sqlforge` on `PATH` in the environment (local TUI, BYOC worker, or custom worker image).
- Warehouse credentials via `.env` / secrets in the workspace (not platform-managed in v1).

## Typical agent flows

| Goal | Command |
|------|---------|
| See pending DDL | `sqlforge plan <env>` |
| Deploy models | `sqlforge apply <env>` — confirm with the user first |
| Run a metric | `sqlforge query <metric> <env>` |
| Historized tables | `sqlforge snapshot <env>` |
| Branching env | `sqlforge env create <name> --base-env prod` |

Default `<env>` comes from `default_environment` in `sqlforge.yml` (fallback `prod`).

## Brain vs SQLForge

- **Drover Brain MCP** — codebase knowledge (how auth works, where files live).
- **SQLForge** — warehouse models, metrics, plan/apply. Do not use Brain for table or metric questions in a SQLForge repo.

Optional sidecar: `sqlforge mcp <env>` exposes read-heavy tools (`list_metrics`, `query_metric`, `plan_change` → `apply_change`) when Gateway MCP routing is configured.

## Custom commands

Project templates under `.drover/commands/`:

- `sqlforge-plan.md` — plan-only review for an environment.
- `sqlforge-apply.md` — apply after explicit user confirmation.

## Hosted agent jobs

Workers need `sqlforge` in the image if the checked-out repo contains `sqlforge.yml`. Apply remains a human or CI gate—agents must not run `sqlforge apply` without confirmation.

## References

- [`drover-sqlforge/CONTEXT.md`](../../../drover-sqlforge/CONTEXT.md)
- [`drover-code/CONTEXT.md`](../../CONTEXT.md) — SQLForge integration
- Implementation: `internal/integrations/sqlforge/`, `internal/config/loader.go`
