# Drover-Code TUI Roadmap 2026: "From Powerful to World-Class"

**Vision:** Build the most reliable, governed, and professional AI agent Terminal User Interface (TUI)—while matching or exceeding the creativity and fluidity of tools like Pi.

## Core Differentiators vs Competitors (e.g. Pi)
| Area | Competitor Strength | Drover-Code Advantage (Target) |
|------|---------------------|--------------------------------|
| **Governance & Safety** | Minimal | **Strong** (Drover Guard + Risk Tiers + Audit) |
| **Reliability & Polish** | Very Good | **Excellent** |
| **Extensibility** | Outstanding | **Strong** (Custom Commands + Guard) |
| **Enterprise Readiness** | Weak | **Best-in-class** |

---

## Phase 1: Foundation (Current - Next 2 Weeks)
*Goal: Match usability standards while adding enterprise safety.*

| Feature | Status | Beats Competition By |
|---------|--------|----------------------|
| **Persistent Input History** | ✅ Done | Reliability (Global Deduplication) |
| **Interactive Diffs (Hunk)** | ✅ Done | Professional control & Native Text Selection |
| **Custom Commands (`/commands`)** | ✅ Done | Usability & Extensibility |
| **Command History Search (`Ctrl+R`)** | ✅ Done | Fluidity & Speed |
| **Message Queuing (`m.agentBusy`)** | ✅ Done | Zero input loss during agent execution |
| **Pause / Resume Agent** | 🟡 Planned | Control & Halting runaway processes |

---

## Phase 2: Professional Polish (Next 1–2 Months)
*Goal: Clearly ahead on polish and governance.*

| Feature | Priority | Why It Beats the Competition |
|---------|----------|------------------------------|
| **Command Palette (`Ctrl+K`)** | ✅ High (foundation shipped) | Semantic actions (ActionKey + Category + Shortcut + RiskLevel), not just text injection. See commandpalette/ and model.go:buildCommandPaletteCommands |
| **Theme System (Dark/Light + Custom)** | High | Enterprise look & accessibility |
| **Session Trees / Branching** | High | Governed branching with Drover Guard |
| **Live Agent Status Bar** | ✅ High (delivered + Guard hooks) | Real-time risk & Guard enforcement status. GuardRiskLevel/Reason on Model, assessPermissionRisk (file + bash patterns), StatusBar renders "● CAUTION (reason)" + high-risk red. See pkg/guardclient + statusbar + model.SetGuardRisk |
| **Audit Log Viewer (in-TUI)** | Medium | Instant compliance visibility |
| **Vim Keybindings Mode** | Medium | Seamless Power-user experience |

---

## Phase 3: Enterprise & Differentiation (2–4 Months)
*Goal: World-class enterprise TUI.*

| Feature | Priority | Why It Beats the Competition |
|---------|----------|------------------------------|
| **Multi-Agent Coordination View** | High | Visual orchestration & delegation (beats simple trees) |
| **Guard-Aware Diff Review** | High | Risk-tiered, fully auditable diff approval |
| **Team Collaboration Mode** | High | Shared TUI sessions with IAM permissions |
| **Compliance Export (PDF/JSON)** | Medium | Instant audit reports for security teams |
| **Accessibility Mode (Screen Reader)** | Medium | True enterprise compliance and inclusivity |

---

## Recommended Immediate Next Steps
1. **Command Palette (`Ctrl+K`)** — High impact on user delight and workflow speed.
2. **Live Status Bar with Guard integration** — Surface risk tiers and active safety checks.
3. **Pause/Resume** — Safely interrupt the agent and supply a reason.
