---
title: Interaction guidelines
description: Conventions for agent prompts, tool use, and developer interaction patterns in Drover Code.
product: drover-code
audience: platform-operator
doc_type: reference
topics:
  - agent-jobs
surface: repo-docs
---

# Interaction Guidelines for drover-code Development

This document helps maintain productive momentum when working with agentic contributors on drover-code and related projects.

## Proactive Execution

When a task is clearly scoped and delegated:

- **Do not pause for additional confirmation** after receiving a directive (e.g., "write design doc", "start implementation", "refactor X").
- **Parse concrete outputs** (file paths, file names, formats) as immediate execution targets, not discussion topics.
- **Begin work immediately** upon receiving a clear task description or acceptance of a proposed direction.

Example:
- ❌ "I'll create a design document about drover-cloud" → (pauses)
- ✅ "Creating design/17-drover-cloud.md with architecture, pricing, deployment phases..." → (starts writing immediately)

## Communication Patterns

- **During active work**: Provide brief status updates in tool calls (e.g., "Writing section 3..." in comments).
- **Upon completion**: Summarize what was written and any next steps clearly.
- **On ambiguity**: Ask a focused clarification question, don't pause indefinitely.

## Repository Conventions

- Design docs follow the numbered pattern: `design/NN-topic.md`
- Each doc has a clear purpose statement and sections mapped to implementation phases.
- Files in progress can be marked with a `[WIP]` prefix in the first heading if needed, removed when complete.
- Commit messages follow conventional commit format (`docs(design): add drover-cloud architecture` etc.).

## Decision Gates

When a user says:
- **"Create X"** or **"Design X"** → Start writing immediately to the specified file path
- **"Implement X"** → Begin coding; ask clarifying questions only if truly ambiguous
- **"What do you think about X?"** → Provide analysis, then propose next steps
- **"Are you building/thinking/working?"** → Assume this is a nudge to start if you haven't; confirm and begin

## Context Continuity

- Read `interaction-guidelines.md` at the start of each conversation about drover-code work
- Check `git_status` before committing to understand what's staged
- Review recent commits with `git_log` to understand project momentum
- When resuming work, scan the most recent design doc or TODO file for context

## File Structure for Work-in-Progress

If a task spans multiple conversations or is explicitly long-form:
- Create the file immediately (even if outline-only)
- Commit with message like `docs: [WIP] drover-cloud design`
- Flesh out sections progressively
- Mark `[WIP]` in the heading until ready for review

This keeps work discoverable and shows momentum even during extended development.
