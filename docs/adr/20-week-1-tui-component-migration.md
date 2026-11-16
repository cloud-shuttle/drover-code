# Week 1: TUI Component Foundation + Polish — Detailed Task List

**Goal**  
Finish the core component migration (skeleton through PermissionPrompt) + deliver noticeable Live Status Bar polish so the TUI feels clean, maintainable, and professional. Zero user-visible behavior change.

**Timebox**  
5–7 working days (part-time or full-time).

**Success Metric**  
- Main Model is visibly smaller and more compositional.
- StatusBar and LiveRegion are real components (no god-model duplication for those areas).
- PermissionPrompt is extracted.
- Live Status Bar shows model + tokens + basic guard/risk indicator.
- All existing tests + snapshots pass.
- Code is ready for Command Palette work in Week 2.

**Dependencies / Context**
- Existing beads: `dcode-002` (skeleton), `dcode-003` (StatusBar), `dcode-004` (LiveRegion+ToolSpinner), `dcode-005` (wiring + dual-state cleanup), `dcode-007` (PermissionPrompt).
- Knowledge: `beads/tui-components.jsonl`
- Design docs: `design/07-tui.md`, `design/18-tui-roadmap.md`
- Current state (as of 2026-05): No `components/` dir yet; all logic still in `model.go`/`view.go`/`permission.go`.

---

## Task Breakdown (Granular, Executable Order)

### 1. Final Prep & Audit (0.5 day)
**Goal**: Confirm exact delta before touching code.

**Tasks**:
- Run full test suite + snapshot diff (`go test ./internal/tui/... -update` only if needed for baseline).
- Create `internal/tui/components/` tree (empty packages) per `beads/tui-components.jsonl` tui-comp-002.
- Add `internal/tui/core/types.go` with `RenderedTurn`, `CompletedTool` (and commented lightweight Component interface).
- Audit every place that touches `agentBusy`, `activeTools`, `stream*`, `total*Tokens`, `permPrompt*`, `textarea`, `history`, `viewport`.

**Files to touch**:
- `internal/tui/components/...` (new dirs)
- `internal/tui/core/types.go` (new)
- `design/20-week-1-tui-component-migration.md` (this file — mark tasks done)

**Acceptance**:
- `go build ./...` clean.
- A one-page "Current State vs Target" diff in the thoughts for dcode-002.

**Linked bead**: dcode-002 → broken into **dcode-011** (prep/audit/skeleton) + **dcode-012** (ToolSpinner) + **dcode-013** (LiveRegion core)

---

### 2. Implement ToolSpinner + LiveRegion Core (1–1.5 days) — Highest leverage
**Goal**: Own the live activity area (the thing users stare at most).

**Detailed sub-tasks**:
2.1 Create `components/toolspinner/toolspinner.go` (struct + New + View) — exact shape from `beads/tui-components.jsonl` tui-comp-005.
2.2 Move `lastLines` and `softenAssistantParagraphBreaks` helpers into `liveregion/` (or keep as shared util).
2.3 Implement `components/liveregion/liveregion.go`:
   - `ActiveTools map[int]*toolspinner.ToolSpinner`
   - `ToolOrder []int`
   - Streaming preview (last N lines, width handling)
   - `View()`, `SetSize()`
2.4 Write isolated component test (`liveregion_test.go`) covering:
   - 0/1/3 active tools
   - Streaming truncation at `liveRegionMaxLines`
   - Error vs success rows
   - Narrow width

**Files**:
- `internal/tui/components/toolspinner/toolspinner.go` + `_test.go`
- `internal/tui/components/liveregion/liveregion.go` + `_test.go`
- `internal/tui/assistant_spacing.go` (possible move or delegation)

**Acceptance**:
- Component renders identically to old `viewLiveRegion()`.
- New tests pass in isolation.

**Linked bead**: dcode-004 (primary)

---

### 3. StatusBar Component + Basic Live Enhancements (1 day)
**Goal**: First visible win + foundation for guard/risk indicators.

**Detailed sub-tasks**:
3.1 Implement `components/statusbar/statusbar.go` (New, SetSize, View) per `beads/tui-components.jsonl` tui-comp-003.
3.2 Add minimal `AgentMode` / `RiskLevel` fields (string or enum) for future guard integration.
3.3 Wire into Model (dual-state sync for `agentBusy`, tokens, modelName).
3.4 Replace `viewStatusBar()` call in `View()`.
3.5 Enhance the rendered output:
   - Model name (already there)
   - Token counts (already there)
   - "● LIVE" when busy (enhance styling)
   - Add placeholder for risk indicator (e.g. colored dot or text "Guard: Normal")

**Files**:
- `internal/tui/components/statusbar/statusbar.go` + `_test.go`
- `internal/tui/model.go` (add field + sync points)
- `internal/tui/view.go` (call site + delete old helper)
- `internal/tui/styles.go` (possible new `styleStatusRisk*` or reuse AdaptiveColor)

**Acceptance**:
- Status bar looks professional and identical (or better).
- Component test covers busy + token formatting + narrow width.
- Risk placeholder renders (even if always "Normal" this week).

**Linked beads**: dcode-003 → split into **dcode-014** (StatusBar core) + **dcode-015** (Live Status Bar enhancements)

---

### 4. Major Wiring + Dual-State Consolidation Pass (1.5–2 days) — Critical
**Goal**: Pay back technical debt and make Model smaller.

**Detailed sub-tasks**:
4.1 Add `*liveregion.LiveRegion` and `*statusbar.StatusBar` (and later Permission) to `Model` struct.
4.2 Update `NewModel()` construction.
4.3 Audit + update every mutation site (handleAgentEvent, UsageEvent, submitInput, DoneEvent, ErrorEvent, agentRunCompleteMsg, etc.).
4.4 Forward `spinner.TickMsg` and relevant key/resize messages where needed.
4.5 Replace all `viewLiveRegion()` / `viewStatusBar()` calls.
4.6 **Consolidation**: Remove (or comment + mark for deletion) the now-duplicated fields:
   - `streaming`, `streamBuf`, `streamLines`
   - `activeTools`, `toolOrder`, `pendingDone`
   - `agentBusy`, `totalInputTokens`, `totalOutputTokens` (after StatusBar owns them)
   - `permPrompt`, `permBatch` (later in Permission task)
6. Update `relayout()` / `viewportHeight()` to work with components (they can report desired height or we keep constants for Week 1).

**Files**:
- `internal/tui/model.go` (biggest diff)
- `internal/tui/view.go`
- `internal/tui/messages.go` (if new component messages needed)

**Acceptance**:
- `go test ./internal/tui/...` — all green, no snapshot changes.
- `Model` struct is noticeably smaller.
- Dual-state comments are clear so dcode-005 can be verified later.

**Linked bead**: dcode-005 → split into **dcode-016** (wiring + dual-state sync) + **dcode-017** (consolidation / field removal)

---

### 5. Extract PermissionPrompt (1 day)
**Goal**: Remove the last major private prompt type.

**Detailed sub-tasks**:
5.1 Create `components/permissionprompt/permissionprompt.go` with both `PermissionPrompt` and `PermissionBatchPrompt`.
5.2 Move `render`, `jsonPreview`, batch truncation logic.
5.3 Add component test for single + batch rendering + preview width handling.
5.4 Wire into Model (dual-state for the two prompt fields).
5.5 Update height reservation logic in `viewportHeight()`.
5.6 Delete (or deprecate) old types in `permission.go`.

**Files**:
- `internal/tui/components/permissionprompt/permissionprompt.go` + `_test.go`
- `internal/tui/model.go` + `view.go`
- `internal/tui/permission.go` (cleanup target)

**Acceptance**:
- Permission prompts (including batch "…and N more") render identically.
- All permission fuzz + manual flows still work.

**Linked bead**: dcode-007

---

### 6. Live Status Bar — Guard/Risk + Polish (0.5–1 day)
**Goal**: Deliver the "Professional Polish" part of Week 1.

**Detailed sub-tasks**:
6.1 Extend `StatusBar` struct with `RiskLevel` (enum or string: "normal" | "caution" | "high") and `Mode` (e.g. "undercover").
6.2 Add simple rendering:
   - Green/yellow/red indicator (use existing `colSuccess` / `colWarning` / `colError` or new AdaptiveColors).
   - Short text like "Guard: Normal" or icon.
6.3 Wire from `Model` (or a future Guard engine hook) — for Week 1 just expose a setter and default to "Normal".
6.4 Update component test and manual verification.

**Files**:
- `components/statusbar/statusbar.go`
- `styles.go` (risk colors if needed)
- `model.go` (sync point)

**Acceptance**:
- Status bar now shows risk/guard status (even if static for now).
- Looks good in both light/dark.

**This directly addresses the "Live Agent Status Bar — In Progress" item in the roadmap.**

---

### 7. Small UX Wins & Bug Fixes (0.5 day)
**Goal**: Make the TUI feel noticeably better this week.

**Tasks** (pick 2–3):
- Better long-stream handling (e.g. force viewport scroll on very long deltas, or cap preview more gracefully).
- Polish permission prompt layout (spacing, borders, readability on narrow terminals).
- Fix any known viewport scrolling jank when permission prompt appears/disappears.
- Improve error banner styling or compaction banner.

**Files**: Mostly `view.go`, `styles.go`, `model.go` (small targeted changes).

**Acceptance**: At least two tangible UX improvements land with no regressions.

---

### 8. Test, Review & Hand-off (0.5 day)
**Tasks**:
- Run full snapshot + e2e suite.
- Manual dogfood session (long agent run with tools, permissions, streaming, history).
- Update `thoughts/dcode-002` through `dcode-007` status + notes with "Week 1 complete" markers.
- Create a short "Week 1 Retro" section in this doc or in `thoughts/dcode-001/`.
- Ensure `go test ./...` and build are clean.

---

## Suggested Execution Order (Risk-Optimized)

1. Prep & skeleton (Task 1)
2. LiveRegion + ToolSpinner (Task 2) — biggest user-visible area
3. StatusBar + basic Live enhancements (Task 3)
4. Big wiring + consolidation (Task 4) — do this while momentum is high
5. PermissionPrompt (Task 5)
6. Live Status Bar risk polish (Task 6)
7. UX wins (Task 7)
8. Test & hand-off (Task 8)

**Parallelizable**:
- PermissionPrompt extraction can start as soon as Task 4 wiring is stable.
- StatusBar risk enhancements can be done while wiring is happening.

---

## Risks & Mitigations

- **Snapshot drift** (highest): Never edit golden files during Week 1. Component tests + manual verification only.
- **Dual-state debt**: Be ruthless about comments and a clear consolidation checklist in Task 4.
- **Spinner tick ownership**: Decide once in Task 2/4 and document in the component.
- **Height reservation coupling**: Keep using the existing constants for Week 1; components report via Model for now.

---

## Definition of Done for Week 1

- All tasks above marked complete in this document + linked beads updated.
- `internal/tui/components/` contains at minimum: `statusbar`, `liveregion`, `toolspinner`, `permissionprompt`.
- `core/types.go` and basic `styles/` organization in place.
- Live Status Bar shows model + tokens + risk/guard indicator.
- Main Model is smaller and the four primary regions (Status, History, Live, Input) have clear component homes (Input/History can be partial).
- Zero user-visible regressions.
- Ready to start Command Palette in Week 2 with a clean architecture.

---

**Execution Status Update (late May 2026)**

Major implementation delivered:

- Skeleton, StatusBar, LiveRegion/ToolSpinner, wiring (002–005)
- Permission full extraction + complete dual-state removal (old permission.go deleted)
- HistoryView full consolidation (legacy m.history/m.viewport removed)
- InputArea + LiveRegion ownership cleanup
- Styles centralization (new internal/tui/styles/ package)

All tests + snapshots green with zero drift. See beads and thoughts/dcode-00X/ for per-bead details.

This document should continue to be updated.

The actual implementation work is tracked in the open foundational beads (dcode-002–005, 007–009). See the new companion document:

→ [design/21-tui-component-implementation-roadmap.md](21-tui-component-implementation-roadmap.md)

**Next**  
After the planning phase, the real work is to start implementing the components. The natural follow-on is still Week 2 Command Palette once the foundation is real, not just planned.

Update this document as you complete tasks. Link PRs or commit hashes next to each task for traceability.

---

## Post-Delivery Hygiene + Reality Record (Crap Analysis Follow-up)

**Trigger:** User-requested "crap analysis and review for feature completeness" after initial Week 1 deliveries (dcode-008 HistoryView + dcode-009 InputArea). The review surfaced that:

- Model had grown to ~1290 LOC during the safe dual-state period (more debt than planned).
- Only dcode-008/009 (and some granular 018-021) had real code + tests; 002-007 were still "open" with planning notes in beads.
- Several "Week 1" ACs (full consolidation, styles centralization, dead-code removal) were incomplete.

**Mandatory Real Consolidation Pass (executed):**
1. Picked LiveRegion + HistoryView first → moved ownership, deleted legacy fields (`m.activeTools`, `m.pendingDone`, `m.toolOrder`, `m.streamLines`, `m.history`, `m.viewport` and all `if m.XXX == nil` paths).
2. PermissionPrompt consolidation + full `internal/tui/permission.go` deletion (old render methods + jsonPreview were dead code after component took over).
3. viewInput / viewAutoComplete removed (0% coverage dead stubs after InputArea wiring).
4. All direct tests updated to use component APIs (no more legacy pokes).
5. Styles centralization into `internal/tui/styles/colors.go` (Col* AdaptiveColors + common lipgloss styles); every component switched over.

**Additional Scope Delivered (beyond original Week 1 plan):**
- Real Guard integration: `pkg/guardclient`, `assessPermissionRisk` (file-sensitive + bash dangerous pattern detection), `SetGuardRisk`, `GuardRiskLevel/Reason` on Model, StatusBar renders risk state ("● CAUTION (reason)", high = red).
- Command Palette foundation: `internal/tui/commandpalette/` with rich `Command` (ActionKey, Category, Shortcut, RiskLevel), semantic actions (not text injection), `buildCommandPaletteCommands` + `executePaletteAction`, Ctrl+K + overlay.
- Coverage: new `strip_test.go` (100% on paste sanitizers: cursor reports, OSC responses, bare numeric fragments, standalone backslashes); renderMarkdown + heavy input paths improved.
- `design/21-tui-component-implementation-roadmap.md` created to document the planning-vs-execution gap.

**Dual-State Technique (what made incremental progress safe):**
Legacy fields and component fields co-existed. All mutations during transition hit both (or used explicit sync helpers like `syncInputArea`). Once every render path, event handler, and test was audited and updated to prefer the component, the legacy branches + fields were deleted in small, reviewable passes. Snapshots never drifted. This contract was the key enabler for the entire migration without breaking 20+ history/snapshot/fuzz/e2e tests.

**Current State (end of hygiene sweep):**
- Four primary regions have dedicated owners (StatusBar, LiveRegion, HistoryView, InputArea).
- PermissionPrompt overlays fully componentized.
- Model is orchestration + delegation; View() is a short composition.
- Zero user-visible change; all tests green.
- Architecture ready for deeper Guard (tool-result risk), custom command registration for palette, full InputArea ownership move, etc.

This document + the beads (dcode-001 now closed, 002-009 updated with reality notes) and AGENTS.md now accurately reflect what shipped vs what was planned. Design/07/18/19 lightly refreshed in the same sweep.
