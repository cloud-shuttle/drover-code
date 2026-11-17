# drover-code roadmap

Last updated 18 Nov 2026. Status: **ACTIVE**. Coding-agent engine and UKC worker runtime. Last code change 11 Nov 2026 (local session harness).

## The Drover gap program

In November 2026 Drover was compared with Vercel eve 1.0. Drover leads on isolation, the model gateway and governance, and trails on the layer people touch. The program closes five gaps across the repos: durable multi-turn sessions (drover-kelpies), a Slack-first channels layer (drover-cloud), unified OpenTelemetry tracing with a run viewer (drover-libs, Cloud console), a TypeScript client SDK (drover-cloud), and an extension bundle format (drover-muster, drover-registry). Four capabilities are deferred to Later: shared multi-person sessions, ambient channel listening, agents editing their own code, and session prewarm and migration.

Program tracking issue: in [cloud-shuttle/drover-cloud](https://github.com/cloud-shuttle/drover-cloud/issues?q=is%3Aissue+label%3Aroadmap+Tracking) (private). Sessions run on DBOS, not Temporal (ADR 0005 in drover-kelpies). Dates and day counts are estimates.

| Milestone | Due (estimate) |
|---|---|
| M0 Truth-up | 27 Nov 2026 |
| M1 Trace spine | 18 Dec 2026 |
| M2 Durable sessions | 5 Feb 2027 |
| M3 Channels and SDK | 5 Mar 2027 |
| M4 Extensions | 2 Apr 2027 |
| Later | after M4 |

## This repo in the program

### M0 Truth-up (due 27 Nov 2026)

- [main does not build from a clean clone: go.mod needs go mod tidy](https://github.com/cloud-shuttle/drover-code/issues/19) (fixed on main by 44a199a; CI guard in #26)
- [Fix Go module path (github.com/cloudshuttle/drover-code) and the warden replace mismatch](https://github.com/cloud-shuttle/drover-code/issues/20) (module path fixed in the module-path PR; the warden replace went away in 44a199a)

### M1 Trace spine (due 18 Dec 2026)

- [Emit run spans and carry traceparent through the worker contract](https://github.com/cloud-shuttle/drover-code/issues/21)
- [Use drover-libs pkg/sessionevent instead of the local copy](https://github.com/cloud-shuttle/drover-code/issues/22)

### M2 Durable sessions (due 5 Feb 2027)

- [Session-aware worker: run one turn of a durable session](https://github.com/cloud-shuttle/drover-code/issues/23)

### Later (after M4)

- [Later: agent proposes changes to its own skills and config as a reviewed PR](https://github.com/cloud-shuttle/drover-code/issues/24)

Each item is a GitHub issue labelled `roadmap` with a `gap:*` label and a milestone: [roadmap issues in this repo](https://github.com/cloud-shuttle/drover-code/issues?q=is%3Aissue+label%3Aroadmap).

## Proposed releases

- `v0.1.0` at M1 (clean build, run spans)
- `v0.2.0` at M2 (session-aware worker)

## Existing roadmap epics

These predate the gap program and stay open:

- [#15 Epic: Test coverage plan](https://github.com/cloud-shuttle/drover-code/issues/15)
- [#14 Epic: TUI component architecture: StatusBar, LiveRegion, ToolSpinner](https://github.com/cloud-shuttle/drover-code/issues/14)
- [#13 Epic: TUI Phase 3: enterprise and differentiation](https://github.com/cloud-shuttle/drover-code/issues/13)
- [#12 Epic: TUI Phase 2: professional polish](https://github.com/cloud-shuttle/drover-code/issues/12)

## How this file is kept current

Update this file in the same PR that closes a roadmap issue or moves a milestone. Status words used across Drover: ACTIVE (code changed in the last 60 days), STALE (no code change for 61 to 180 days), PROTOTYPE (experimental or scaffold), SUPERSEDED (replaced by another repo).
