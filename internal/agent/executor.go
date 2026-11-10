package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/sync/errgroup"

	droverwarden "github.com/cloud-shuttle/drover-warden/warden"
	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/permissions"
	"github.com/cloudshuttle/drover-code/internal/telemetry"
	"github.com/cloudshuttle/drover-code/internal/tools"
	"github.com/cloudshuttle/drover-code/internal/warden"
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

func planModeRefusal(tool string) string {
	return fmt.Sprintf("plan mode refuses %s. read_file, list_directory, file_info, glob, grep, explore, and write_plan still run. Shift+Tab leaves plan mode.", tool)
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
		for i, call := range calls {
			if e.registry.NeedsPermission(call.Name, call.Input) {
				// Allow rules do not override plan mode.
				decisions[i] = tools.Deny
				continue
			}
			decisions[i] = tools.Allow
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
			content := "Tool execution denied by user."
			summary := "denied by user"
			if e.perm != nil && e.perm.Mode() == permissions.ModePlan {
				content = planModeRefusal(call.Name)
				summary = "refused by plan mode"
			}
			e.emit(ToolDoneEvent{
				CallIndex:     idx,
				ID:            call.ID,
				Name:          call.Name,
				IsError:       true,
				OutputSummary: summary,
			})
			return api.ToolResultBlock{
				ToolUseID: call.ID,
				Content:   content,
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

	text, images := api.UnpackImages(output)
	if call.Name == "todo" {
		e.emit(TodoEvent{Text: text})
	}
	return api.ToolResultBlock{
		ToolUseID: call.ID,
		Content:   text,
		Images:    images,
		IsError:   false,
	}, nil
}
