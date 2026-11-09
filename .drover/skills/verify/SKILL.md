---
name: verify
description: Run verifier-per-PR smoke journeys with evidence. Use after an approved PR head SHA is ready.
product: drover-code
audience: agent
---

# Verify (verifier-per-PR)

Drive an isolated verification run against an approved PR head SHA. The verifier cannot edit source/oracles or push/merge.

## Triggers

- PR head SHA ready for evidence
- User says "verify", "run harness", or "smoke the PR"
- After a fixer returns a patch (rebuild + reverify)

## Steps

1. Confirm head SHA freshness (`git rev-parse HEAD` must match the approved SHA).
2. Isolated build → health/freshness → affected CLI/API journeys → evidence verdict.
3. Use synthetic tenant data and restricted egress; no production/Git-write credentials.
4. Missing evidence or skipped required paths are **blocked**, not green.
5. If fail: schedule a **fixer** sibling (does not consume an extra nesting tier). Max **two** repair attempts, **20 minutes** total, then escalate.
6. Persist evidence under `docs/tasks/<id>/evidence/<sha>.json`.
7. Never auto-merge.

## Done when

- Evidence JSON exists with `verdict=pass|blocked|fail`, SHA, build digest and step results
- Machine checks outrank model declarations

Runtime verification APIs live in `internal/verification`. This skill is a recipe only.
