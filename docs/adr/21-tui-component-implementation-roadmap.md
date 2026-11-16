# TUI Component Implementation Roadmap (Post Week 1 Planning)

**Date:** 2026-05-28  
**Context:** We did an excellent job creating a detailed, granular plan for Week 1 (dcode-011 → dcode-021). However, most of those sub-beads were completed at the *tracking and planning* level only. Very little actual source code has been written yet.

**Current Reality (as of now):**
- 13 of 21 dcode-* beads are marked "done".
- Only **dcode-020** delivered real product code changes.
- The core implementation work (creating `internal/tui/components/`, wiring, tests) is still ahead of us.
- The granular sub-beads (011-021) are now the **detailed execution plan** for the remaining work.

---

## Recommended Approach Going Forward

### 1. Treat the Foundational Beads as the Real Work
Focus on these 7 open tickets (in rough dependency order):

| Priority | Bead | What It Actually Delivers | Granular Sub-beads That Map To It |
|----------|------|---------------------------|-----------------------------------|
| 1 | dcode-002 | Skeleton + core/types.go + directory structure | dcode-011 |
| 2 | dcode-003 | Real StatusBar component + test | dcode-014 + dcode-015 |
| 3 | dcode-004 | Real LiveRegion + ToolSpinner + tests | dcode-012 + dcode-013 |
| 4 | dcode-005 | Wiring + dual-state cleanup + Model cleanup | dcode-016 + dcode-017 |
| 5 | dcode-007 | PermissionPrompt component + test | dcode-018 + dcode-019 |
| 6 | dcode-008 | HistoryView component + test | (new sub-beads recommended) |
| 7 | dcode-009 | InputArea component + test | (new sub-beads recommended) |

### 2. Repurpose the Existing Granular Sub-beads
The dcode-011–dcode-021 beads are valuable. Instead of leaving them as "done" planning artifacts, we should either:

**Option A (Recommended):** Re-open the relevant ones as children of the foundational beads above, or
**Option B:** Keep them as done "planning" tickets and create new execution tickets that reference them.

I recommend **Option A** for clarity.

### 3. Execution Cadence
- Work in small, reviewable PRs (1–2 components at a time).
- Every component should have its isolated test before being wired.
- Dual-state period is acceptable during dcode-005, but must be cleaned up before that bead closes.
- Run `/beads-queue drover-code` regularly (AFK or HITL) so the queue reflects real progress.

---

## Suggested Next 4–6 Weeks

**Milestone A: Core Live Area Live (2–3 weeks)**
- dcode-002 + dcode-003 + dcode-004
- Goal: StatusBar + LiveRegion + ToolSpinner actually working in the TUI with tests.

**Milestone B: Wiring & Permission (1–2 weeks)**
- dcode-005 + dcode-007
- Goal: Clean Model, dual-state removed for live/status/permission areas.

**Milestone C: Remaining Major Sections (2+ weeks)**
- dcode-008 + dcode-009
- Goal: HistoryView and InputArea extracted.

**Milestone D: Polish & Close Epic**
- dcode-001 final review + knowledge bead sync + docs.

---

## Immediate Recommended Actions (This Week)

1. **Update dcode-001** (this epic) notes to reflect the new reality (planning vs implementation gap).
2. **Update the 7 open foundational beads** (002–005, 007–009) to reference their granular sub-beads as the execution plan.
3. **Re-open or re-scope** dcode-011–dcode-019 as children of the above (or create execution counterparts).
4. Pick the first real implementation ticket (strongly recommend starting with **dcode-002** or **dcode-003**).
5. Run `/beads-queue drover-code` to get a fresh view of the queue.

---

## Open Questions for the Team

- Do we want to keep the very fine-grained sub-beads (one per small task), or consolidate some now that we have real implementation momentum?
- Should we create a few new "execution" sub-beads under dcode-008 and dcode-009 to match the pattern we used for the earlier components?
- How aggressive do we want to be about dual-state debt during dcode-005?

---

**Bottom line:**  
The planning phase for Week 1 was excellent. Now we need to shift from "planning the work" to "working the plan." The next real value will come from opening an editor and starting to build the actual components under `internal/tui/components/`.

This document can serve as the north star for the implementation phase.