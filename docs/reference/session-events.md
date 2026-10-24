---
title: Session events
description: JSONL schema Drover Code writes beside an issue-run session.
product: drover-code
audience:
  - platform-operator
  - agent
doc_type: reference
topics:
  - agent-jobs
  - observability
surface: repo-docs
---

# Session events

Set `DROVER_SESSION_EVENTS` to a file path. Drover Code appends one JSON object per line. `v` is `1`.

| Field | Meaning |
|---|---|
| `type` | `session.started`, `session.status`, `message.appended`, `tool.requested`, `tool.finished`, `approval.requested`, `approval.resolved`, `artifact.written`, `session.linked` |
| `session_id` | From `DROVER_SESSION_ID`, or `local` |
| `issue_id` | From `DROVER_ISSUE_ID` |
| `phase` | From `DROVER_PHASE` |
| `data` | Type-specific fields, such as `status` or tool `name` |

`DROVER_SESSION_EVENTS_ENVELOPE=0` tells Drover Code not to write `session.started`, `session.linked`, or the opening `running` status. The orchestrator or `dvr host` writes that envelope and Drover Code appends tool events to the same file.

This file is not the worker **execution stream** and not the Cloud **job event log**.
