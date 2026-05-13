# Drover-Code TUI Test Strategy

**Version:** 1.0  
**Date:** May 13, 2026  
**Status:** Approved  
**Goal:** Build and maintain the most reliable, professional, and governed AI agent TUI in the industry.

-----

### 1. Executive Summary

Drover-Code’s TUI is built on **BubbleTea** (Go) and aims to combine **enterprise-grade governance** (via Drover Guard) with **excellent usability**.

We will adopt the strongest testing practices from:

- **OpenHands** → Snapshot + visual regression testing
- **Pi** → Extension-style, agent-driven testing
- **Aider** → Subprocess + output capture for integration tests
- **Plandex** → Focused interactive diff / hunk selection testing

This strategy ensures high confidence in TUI behavior, especially for complex features like Interactive Diffs, Custom Commands, and Input History.

-----

### 2. Learnings from Other Projects

|Project  |Key Strength                             |How We Apply It                                  |
|---------|-----------------------------------------|-------------------------------------------------|
|OpenHands|Snapshot + visual regression testing     |Golden master tests for markdown & diff rendering|
|Pi       |Extension-driven, agent-tested UI        |Test harness for custom commands                 |
|Aider    |Subprocess + output capture              |Robust E2E TUI interaction tests                 |
|Plandex  |Interactive diff / hunk selection testing|Dedicated tests for diff review flows            |

-----

### 3. Test Types & Coverage Goals

|Test Type         |Coverage Goal  |Framework / Tool                |Frequency|
|------------------|---------------|--------------------------------|---------|
|Unit Tests        |95%+           |Go `testing` + testify          |Every PR |
|Component Tests   |90%+           |BubbleTea message simulation    |Every PR |
|Snapshot / Visual |All major views|Golden files + custom comparator|Every PR |
|Integration Tests |Core flows     |Subprocess + output capture     |Daily    |
|E2E / Behavioral  |Critical paths |Real agent + TUI simulation     |Weekly   |
|Manual Smoke Tests|New features   |Human checklist                 |Release  |

-----

### 4. Comprehensive Test Suites

#### 4.1 Input & Navigation

- Input History (Up/Down + fuzzy search)
- Multi-line input & paste handling
- Draft preservation
- Command palette (`Ctrl+K`)
- Fuzzy history search (`Ctrl+R`)

#### 4.2 Interactive Diffs (High Priority)

- Hunk parsing correctness
- Visual rendering & selection
- Accept / Reject / Accept-All / Reject-All flows
- Safe patch application (no partial writes)
- Guard integration before/after apply

#### 4.3 Custom Commands

- Command loading & parsing (markdown + JSON)
- Template expansion (`$1`, `@file`, `!shell`)
- Guard evaluation for every command
- `/commands init` and `/commands list`

#### 4.4 Agent Interaction

- Message queueing while agent is busy
- Interrupt handling (`Ctrl+C`)
- Pause / Resume
- Live status updates

#### 4.5 Rendering & UX

- Markdown rendering (Glamour)
- Terminal resize handling
- Theme switching
- Long session stability (500+ turns)

-----

### 5. Implementation Plan (Next 4 Weeks)

**Week 1**

- Snapshot testing infrastructure for markdown & diffs
- Expand existing `model_test.go` with history & fuzzy search tests

**Week 2**

- Interactive Diffs dedicated test suite
- Subprocess-based E2E tests (inspired by Aider)

**Week 3**

- Custom Commands test harness
- Guard integration tests for commands

**Week 4**

- Full CI integration + visual regression checks

-----

### 6. Tools & Frameworks

- **Unit/Component**: Go `testing` + `testify` + BubbleTea message simulation
- **Snapshot**: Custom golden file comparator (or `github.com/sergi/go-diff`)
- **E2E**: `github.com/ory/dockertest` + subprocess execution
- **CI**: GitHub Actions with terminal capture
