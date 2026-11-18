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
| `trace_id`, `span_id` | W3C IDs (lowercase hex) of the headless run's `invoke_agent drover-code` span, on every event. Omitted when the run has no trace context (no `TRACEPARENT` from the parent and no collector configured). Join on `trace_id` to find the run in `otel_traces`. |

`DROVER_SESSION_EVENTS_ENVELOPE=0` tells Drover Code not to write `session.started`, `session.linked`, or the opening `running` status. The orchestrator or `dvr host` writes that envelope and Drover Code appends tool events to the same file.

The schema lives in [`drover-libs/pkg/sessionevent`](https://github.com/cloud-shuttle/drover-libs/tree/main/pkg/sessionevent). drover writes the same format.

## Tracing

Headless runs start OpenTelemetry through `drover-libs/pkg/telemetry`. When the parent passes `TRACEPARENT` (Cloud does this for every agent job), the run's span is a child of the Cloud job span. Export is on only when a collector is configured: `OTEL_EXPORTER_OTLP_ENDPOINT` (traces then default to `http/protobuf`, the same protocol as the Warden OTel logs) or `DROVER_OTEL_ENDPOINT=host:port` (gRPC). `DROVER_OTEL_ENABLED=false` turns export off.

This file is not the worker **execution stream** and not the Cloud **job event log**.
