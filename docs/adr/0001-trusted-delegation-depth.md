---
title: "ADR 0001: Trusted delegation depth ceiling"
description: Enforce maxAgentTiers=3 with inherited lineage; repo config may tighten only.
product: drover-code
audience: platform-operator
doc_type: adr
topics:
  - agent-jobs
  - governance-policy
surface: repo-docs
---
# ADR 0001: Trusted delegation depth ceiling

**Status**: Accepted  
**Date**: 2026-11  
**Deciders**: Platform Architecture Team

## Context

Coordinator fan-out, turn counters and compaction rounds are not nesting guards. Without an inherited depth field, child agents can recurse past a safe delegation ceiling. HumanLayer's CodeLayer practice delegates at most three levels deep; Drover adopts an explicit local policy of root depth 0 with a hard ceiling of three tiers.

## Decision

1. Add trusted `Lineage` on every agent loop, minted only by trusted ingress at depth 0.
2. Children inherit depth, root ID, approved spec hash, scope and remaining deadline/budget.
3. Spawn admission denies when `parent.Depth+1 >= maxAgentTiers`.
4. Operator ceiling is `maxAgentTiers=3`. Repository settings may tighten, never raise.
5. Prompts, environment variables and child-supplied JSON cannot reset lineage or mint a root.

## Consequences

- Depth 0→1 and 1→2 succeed; 2→3 fails before provisioning.
- Forged or missing lineage is denied.
- Independent concurrency, time and spend caps remain separate controls.
- Skills under `.drover/skills/` are recipes only; runtime gates stay mandatory when skills are skipped.

## Supersedes

None.
