package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/sync/errgroup"

	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/permissions"
	"github.com/cloudshuttle/drover-code/internal/telemetry"
	"github.com/cloudshuttle/drover-code/internal/tools"
	"github.com/cloudshuttle/drover-code/internal/warden"
	droverwarden "github.com/cloud-shuttle/drover-warden/warden"
)

type ToolExecutor interface {
	ExecuteTools(ctx context.Context, calls []api.ToolUseBlock) ([]api.ToolResultBlock, error)
}

type DefaultToolExecutor struct {
	registry *tools.Registry
	perm     *permissions.Engine
	eventCh  chan<- Event
}

func NewDefaultToolExecutor(registry *tools.Registry, perm *permissions.Engine, eventCh chan<- Event) *DefaultToolExecutor {
	return &DefaultToolExecutor{
		registry: registry,
		perm:     perm,
		eventCh:  eventCh,
	}
}

func (e *DefaultToolExecutor) emit(ev Event) {
	if e.eventCh != nil {
		e.eventCh <- ev
	}
}

func (e *DefaultToolExecutor) ExecuteTools(ctx context.Context, calls []api.ToolUseBlock) ([]api.ToolResultBlock, error) {
	results := make([]api.ToolResultBlock, len(calls))

	decisions := make([]tools.Decision, len(calls))
	for i := range decisions {
		decisions[i] = tools.Decision(-1) // unknown
	}
	if e.perm != nil && e.perm.Mode() == permissions.ModePlan {
		var batchIdxs []int
		var batchItems []PermissionBatchItem

		for i, call := range calls {
			if !e.registry.NeedsPermission(call.Name, call.Input) {
				decisions[i] = tools.Allow
				continue
			}

			if d, ok := e.perm.FastDecision(call.Name); ok {
				decisions[i] = d
				continue
			}

			batchIdxs = append(batchIdxs, i)
			batchItems = append(batchItems, PermissionBatchItem{
				ToolName: call.Name,
				Input:    call.Input,
				Summary:  summariseInput(call.Name, call.Input),
			})
		}

		if len(batchItems) > 0 {
			respCh := make(chan PermissionDecision, 1)
			select {
			case e.eventCh <- PermissionBatchRequestEvent{Items: batchItems, DecisionCh: respCh}:
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			var d PermissionDecision
			select {
			case d = <-respCh:
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			switch d {
			case PermAllow:
				for _, idx := range batchIdxs {
					decisions[idx] = tools.Allow
				}
			case PermAlwaysAllow:
				for _, idx := range batchIdxs {
					decisions[idx] = tools.Allow
					e.perm.PersistAllow(calls[idx].Name)
				}
			default:
				for _, idx := range batchIdxs {
					decisions[idx] = tools.Deny
				}
			}
		}
	}

	g, gctx := errgroup.WithContext(ctx)

	for i, call := range calls {
		i, call := i, call

		g.Go(func() error {
			result, err := e.executeSingleTool(gctx, i, call, decisions)
			if err != nil {
				return err
			}
			results[i] = result
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

func (e *DefaultToolExecutor) executeSingleTool(ctx context.Context, idx int, call api.ToolUseBlock, preDecisions []tools.Decision) (api.ToolResultBlock, error) {
	if idx < len(preDecisions) && preDecisions[idx] != tools.Decision(-1) {
		if preDecisions[idx] == tools.Deny {
			e.emit(ToolDoneEvent{
				CallIndex:     idx,
				ID:            call.ID,
				Name:          call.Name,
				IsError:       true,
				OutputSummary: "denied by user",
			})
			return api.ToolResultBlock{
				ToolUseID: call.ID,
				Content:   "Tool execution denied by user.",
				IsError:   true,
			}, nil
		}
		if preDecisions[idx] == tools.AppliedManually {
			e.emit(ToolDoneEvent{
				CallIndex:     idx,
				ID:            call.ID,
				Name:          call.Name,
				IsError:       false,
				OutputSummary: "applied interactively",
			})
			return api.ToolResultBlock{
				ToolUseID: call.ID,
				Content:   "Changes applied interactively by the user via Interactive Diff.",
				IsError:   false,
			}, nil
		}
		goto exec
	}

	if e.registry.NeedsPermission(call.Name, call.Input) && e.perm != nil {
		decision, _ := e.perm.Check(ctx, call.Name, call.Input)

		if decision == tools.Deny {
			e.emit(ToolDoneEvent{
				CallIndex:     idx,
				ID:            call.ID,
				Name:          call.Name,
				IsError:       true,
				OutputSummary: "denied by user",
			})
			return api.ToolResultBlock{
				ToolUseID: call.ID,
				Content:   "Tool execution denied by user.",
				IsError:   true,
			}, nil
		}
		if decision == tools.AppliedManually {
			e.emit(ToolDoneEvent{
				CallIndex:     idx,
				ID:            call.ID,
				Name:          call.Name,
				IsError:       false,
				OutputSummary: "applied interactively",
			})
			return api.ToolResultBlock{
				ToolUseID: call.ID,
				Content:   "Changes applied interactively by the user via Interactive Diff.",
				IsError:   false,
			}, nil
		}
	}

exec:
	e.emit(ToolStartEvent{
		CallIndex:    idx,
		ID:           call.ID,
		Name:         call.Name,
		InputSummary: summariseInput(call.Name, call.Input),
	})

	tr := telemetry.TracerFrom(ctx)
	tid := telemetry.TraceIDFrom(ctx)
	parentGen := telemetry.SpanIDFrom(ctx)
	spanID := tr.StartSpan(telemetry.SpanParams{
		TraceID:  tid,
		ParentID: parentGen,
		Name:     call.Name,
		Input:    json.RawMessage(call.Input),
	})

	wdec := warden.CheckAction(ctx, &droverwarden.GuardRequest{
		TenantID: os.Getenv("DROVER_TENANT_ID"),
		ToolCall: &droverwarden.ToolCall{
			ToolName: call.Name,
			Args:     func() map[string]any { var a map[string]any; _ = json.Unmarshal(call.Input, &a); return a }(),
		},
		Context: map[string]any{
			"agent_id":  os.Getenv("DROVER_AGENT_ID"),
			"raw_input": string(call.Input),
		},
	})
	if !wdec.Allowed {
		execErr := fmt.Errorf("tool blocked by Warden: %s", wdec.Result.Reason)
		summary := truncate(execErr.Error(), 80)
		tr.EndSpan(spanID, telemetry.SpanResult{Output: summary, IsError: true, Error: execErr})
		e.emit(ToolDoneEvent{CallIndex: idx, ID: call.ID, Name: call.Name, IsError: true, OutputSummary: summary})
		return api.ToolResultBlock{ToolUseID: call.ID, Content: execErr.Error(), IsError: true}, nil
	}

	output, execErr := e.registry.Execute(ctx, call.Name, call.Input)

	if execErr != nil {
		summary := truncate(execErr.Error(), 80)
		tr.EndSpan(spanID, telemetry.SpanResult{
			Output:  summary,
			IsError: true,
			Error:   execErr,
		})
		e.emit(ToolDoneEvent{
			CallIndex:     idx,
			ID:            call.ID,
			Name:          call.Name,
			IsError:       true,
			OutputSummary: summary,
		})
		return api.ToolResultBlock{
			ToolUseID: call.ID,
			Content:   execErr.Error(),
			IsError:   true,
		}, nil
	}

	tr.EndSpan(spanID, telemetry.SpanResult{
		Output:  truncate(output, 2048),
		IsError: false,
	})

	e.emit(ToolDoneEvent{
		CallIndex:     idx,
		ID:            call.ID,
		Name:          call.Name,
		IsError:       false,
		OutputSummary: truncate(output, 80),
	})

	return api.ToolResultBlock{
		ToolUseID: call.ID,
		Content:   output,
		IsError:   false,
	}, nil
}
