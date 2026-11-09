---
name: grill
description: Stress-test a plan until shared understanding is confirmed. Use before non-trivial implementation.
product: drover-code
audience: agent
---

# Grill (alignment interview)

Interview until shared understanding is confirmed. Map decisions as a design tree and work the frontier in rounds.

## Triggers

- Non-trivial / multi-session work
- User says "grill", "stress-test the plan", or "align before coding"
- Harness gate requires a SPEC for single- or multi-session work

## Steps

1. Classify work size: `small` (reversible, low wrongness-cost), `single`, or `multi`. Security/data changes never auto-qualify as small.
2. For `small`: record `skip_reason` and stop interviewing.
3. For `single`/`multi`: ask the frontier round (goal, non-goals, constraints, touched boundaries, acceptance checks). Number questions; recommend an answer for each; wait.
4. Write `docs/tasks/<id>/SPEC.md`. Do not treat a model-written "approved" label as acceptance.
5. Stop. Trusted ingress (operator / Cloud) must Accept the SPEC hash before mutating tools.

## Done when

- SPEC exists with goal, non-goals, constraints, boundaries and acceptance checks
- Approver is recorded in `CHECKPOINT.json` by trusted ingress
- Runtime harness gate will allow mutation

Runtime gates remain mandatory even if this skill is skipped.
