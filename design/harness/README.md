# Harness discipline (feature map)

Implements the 2026-11-09 Drover harness design in this repository.

| Concern | Location |
|--------|----------|
| Trusted delegation depth | `internal/agent/lineage.go` (wired in `internal/coordinator`) |
| Alignment gate | `internal/harness/preflight.go` (wired in `internal/agent/executor.go`) |
| Verifier-per-PR | `internal/verification` |
| Context budget | `internal/convo/budget.go` |
| ADR loading | `internal/config/adr_loader.go`, `docs/adr/` |
| Skill recipes | `.drover/skills/{grill,verify}/SKILL.md` via `internal/commands/skills.go` |
| CI check | `.github/workflows/ci.yml` job `harness-discipline` |

Runtime gates remain mandatory even when skills are skipped.
