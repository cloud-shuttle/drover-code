package main

import (
	"encoding/json"
	"os"
	"time"

	"github.com/cloudshuttle/drover-code/internal/agent"
)

// drainHeadlessJSONL writes one JSON object per line to stdout for each agent
// event (design/11-headless-orchestration.md § Phase 2).
func drainHeadlessJSONL(ch <-chan agent.Event) {
	enc := json.NewEncoder(os.Stdout)
	for ev := range ch {
		ts := time.Now().UTC().Format(time.RFC3339Nano)
		switch e := ev.(type) {
		case agent.TextDeltaEvent:
			_ = enc.Encode(map[string]any{
				"type": "text_delta",
				"ts":   ts,
				"text": e.Text,
			})
		case agent.ToolStartEvent:
			_ = enc.Encode(map[string]any{
				"type":          "tool_start",
				"ts":            ts,
				"call_index":    e.CallIndex,
				"id":            e.ID,
				"name":          e.Name,
				"input_summary": e.InputSummary,
			})
		case agent.ToolDoneEvent:
			_ = enc.Encode(map[string]any{
				"type":           "tool_done",
				"ts":             ts,
				"call_index":     e.CallIndex,
				"id":             e.ID,
				"name":           e.Name,
				"is_error":       e.IsError,
				"output_summary": e.OutputSummary,
			})
		case agent.UsageEvent:
			_ = enc.Encode(map[string]any{
				"type":                "usage",
				"ts":                  ts,
				"input_tokens":        e.InputTokens,
				"output_tokens":       e.OutputTokens,
				"total_input_tokens":  e.TotalInputTokens,
				"total_output_tokens": e.TotalOutputTokens,
			})
		case agent.DoneEvent:
			_ = enc.Encode(map[string]any{
				"type": "done",
				"ts":   ts,
			})
		case agent.ErrorEvent:
			_ = enc.Encode(map[string]any{
				"type":    "error",
				"ts":      ts,
				"message": e.Err.Error(),
			})
		case agent.HeartbeatEvent:
			_ = enc.Encode(map[string]any{
				"type": "heartbeat",
				"ts":   e.Time.UTC().Format(time.RFC3339Nano),
				"turn": e.Turn,
			})
		case agent.PermissionRequestEvent:
			_ = enc.Encode(map[string]any{
				"type":      "permission_request",
				"ts":        ts,
				"tool_name": e.ToolName,
				"summary":   e.Summary,
			})
		case agent.PermissionBatchRequestEvent:
			_ = enc.Encode(map[string]any{
				"type": "permission_batch_request",
				"ts":   ts,
				"size": len(e.Items),
			})
		case agent.CompactionStartEvent:
			_ = enc.Encode(map[string]any{
				"type":                    "compaction_start",
				"ts":                      ts,
				"round":                   e.Round,
				"max_rounds":              e.MaxRounds,
				"estimated_tokens_before": e.EstimatedTokensBefore,
			})
		case agent.CompactionDoneEvent:
			row := map[string]any{
				"type":                   "compaction_done",
				"ts":                     ts,
				"round":                  e.Round,
				"estimated_tokens_after": e.EstimatedTokensAfter,
				"duration_ms":            e.Duration.Milliseconds(),
			}
			if e.Err != nil {
				row["error"] = e.Err.Error()
			}
			_ = enc.Encode(row)
		}
	}
}
